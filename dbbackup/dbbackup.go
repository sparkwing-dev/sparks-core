// Package dbbackup dumps a database to a gzipped plain-SQL artifact,
// ships it to a local directory or an s3:// / gs:// prefix, restores it,
// and verifies the restore. Config.Engine selects postgres or mysql.
// Uploads and restore replays honor SPARKWING_DRY_RUN; dumps and
// downloads always run.
package dbbackup

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
	"github.com/sparkwing-dev/sparks-core/step"
)

const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
)

// Config drives Dump and Restore. A zero Engine resolves to postgres.
type Config struct {
	// Engine is "postgres" (default) or "mysql".
	Engine string
	// DSN is a libpq URI/key=value string, or for mysql a mysql:// URL or
	// the go-sql-driver user:pass@tcp(host:port)/db form.
	DSN string
	// Dest is the Dump destination: a local directory, s3://bucket/prefix,
	// or gs://bucket/prefix.
	Dest string
	// Source is the Restore source: a local .sql.gz path or an s3:// / gs://
	// object URL.
	Source string
	// AWSProfile names the profile for s3://; empty uses the default
	// credential chain or IRSA.
	AWSProfile string
	// Project is the GCP project for gs://; empty omits --project.
	Project string
	// Filename overrides the generated `<db>-<timestamp>.sql.gz` basename.
	Filename string
	// WorkDir is the scratch directory for the intermediate dump,
	// defaulting to the OS temp dir.
	WorkDir string
	// DumpArgs are passed through verbatim after the built-in pg_dump /
	// mysqldump flags.
	DumpArgs []string
	// RestoreArgs are passed through verbatim to the psql / mysql client.
	RestoreArgs []string
}

// Artifact is a handle to a produced backup: its final URI and its
// compressed size in bytes. Its URI is usable as a later Restore's Source.
type Artifact struct {
	URI   string
	Bytes int64
	// AWSProfile and Project record the delivery credential context so a
	// RestoreFunc rollback fetches the object back the same way.
	AWSProfile string
	Project    string
}

func (c *Config) engine() string {
	if c.Engine == "" {
		return EnginePostgres
	}
	return c.Engine
}

// Dump dumps Config.DSN to a gzipped `<db>-<timestamp>.sql.gz` artifact
// and delivers it to Config.Dest.
func Dump(ctx context.Context, cfg Config) (Artifact, error) {
	var art Artifact
	engine, err := normalizeEngine(cfg.engine())
	if err != nil {
		return art, err
	}
	if cfg.DSN == "" {
		return art, fmt.Errorf("dbbackup: DSN is required")
	}
	if cfg.Dest == "" {
		return art, fmt.Errorf("dbbackup: Dest is required")
	}
	dest, err := classifyLocation(cfg.Dest)
	if err != nil {
		return art, err
	}
	name := cfg.Filename
	if name == "" {
		name = dumpFilename(dbNameFromDSN(engine, cfg.DSN), time.Now().UTC())
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}

	err = step.Run(ctx, "db-dump ("+engine+")", func(ctx context.Context) error {
		stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
		sqlPath := filepath.Join(workDir, "dbbackup-dump-"+stamp+".sql")
		gzPath := filepath.Join(workDir, "dbbackup-dump-"+stamp+".sql.gz")
		if err := dumpSQL(ctx, engine, cfg, sqlPath); err != nil {
			return err
		}
		defer func() { _ = os.Remove(sqlPath) }()
		size, err := gzipFile(sqlPath, gzPath)
		if err != nil {
			return fmt.Errorf("dbbackup: compress dump: %w", err)
		}
		defer func() { _ = os.Remove(gzPath) }()
		art.Bytes = size
		art.AWSProfile = cfg.AWSProfile
		art.Project = cfg.Project
		art.URI, err = deliver(ctx, cfg, dest, gzPath, name)
		if err != nil {
			return err
		}
		sparkwing.Info(ctx, "dumped %s (%d bytes) -> %s", engine, size, art.URI)
		return nil
	})
	return art, err
}

// Restore pulls Config.Source, decompresses it, and replays it into
// Config.DSN.
func Restore(ctx context.Context, cfg Config) error {
	engine, err := normalizeEngine(cfg.engine())
	if err != nil {
		return err
	}
	if cfg.DSN == "" {
		return fmt.Errorf("dbbackup: DSN is required")
	}
	if cfg.Source == "" {
		return fmt.Errorf("dbbackup: Source is required")
	}
	src, err := classifyLocation(cfg.Source)
	if err != nil {
		return err
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = os.TempDir()
	}

	return step.Run(ctx, "db-restore ("+engine+")", func(ctx context.Context) error {
		gzPath, cleanup, err := fetch(ctx, cfg, src, workDir)
		if err != nil {
			return err
		}
		defer cleanup()
		sqlPath := filepath.Join(workDir, "dbbackup-restore-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql")
		if err := gunzipFile(gzPath, sqlPath); err != nil {
			return fmt.Errorf("dbbackup: decompress source: %w", err)
		}
		defer func() { _ = os.Remove(sqlPath) }()
		if err := replaySQL(ctx, engine, cfg, sqlPath); err != nil {
			return err
		}
		sparkwing.Info(ctx, "restored %s into target database", engine)
		return nil
	})
}

// RestoreFunc returns an OnFailure-shaped closure that restores art back
// into dsn.
func RestoreFunc(art Artifact, engine, dsn string) func(context.Context) error {
	return func(ctx context.Context) error {
		return Restore(ctx, Config{
			Engine:     engine,
			DSN:        dsn,
			Source:     art.URI,
			AWSProfile: art.AWSProfile,
			Project:    art.Project,
		})
	}
}

type VerifyConfig struct {
	// Engine is "postgres" (default) or "mysql".
	Engine string
	DSN    string
	// Query defaults to "SELECT 1".
	Query string
	// MinRows, when > 0, requires the first cell of the first row to parse
	// as an integer >= MinRows. When 0, any error-free result passes.
	MinRows int
}

func (c *VerifyConfig) engine() string {
	if c.Engine == "" {
		return EnginePostgres
	}
	return c.Engine
}

// VerifyRestore runs VerifyConfig.Query against the database and reports
// whether the restore looks healthy.
func VerifyRestore(ctx context.Context, cfg VerifyConfig) error {
	engine, err := normalizeEngine(cfg.engine())
	if err != nil {
		return err
	}
	if cfg.DSN == "" {
		return fmt.Errorf("dbbackup: DSN is required")
	}
	query := cfg.Query
	if query == "" {
		query = "SELECT 1"
	}
	return step.Run(ctx, "verify-restore ("+engine+")", func(ctx context.Context) error {
		var out string
		switch engine {
		case EnginePostgres:
			cleanDSN, env := pgConn(cfg.DSN)
			out, err = sparkwing.Exec(ctx, "psql", pgVerifyArgs(cleanDSN, query)...).EnvMap(env).String()
		case EngineMySQL:
			conn, perr := parseMySQLDSN(cfg.DSN)
			if perr != nil {
				return perr
			}
			args, env := mysqlVerifyArgs(conn, query)
			out, err = sparkwing.Exec(ctx, "mysql", args...).EnvMap(env).String()
		}
		if err != nil {
			return fmt.Errorf("dbbackup: verify query failed: %w", err)
		}
		if cfg.MinRows > 0 {
			n, perr := parseRowCount(out)
			if perr != nil {
				return fmt.Errorf("dbbackup: verify expected a numeric count: %w", perr)
			}
			if n < cfg.MinRows {
				return fmt.Errorf("dbbackup: verify got %d rows, want >= %d", n, cfg.MinRows)
			}
			sparkwing.Info(ctx, "verify passed: %d rows (>= %d)", n, cfg.MinRows)
			return nil
		}
		sparkwing.Info(ctx, "verify passed")
		return nil
	})
}

func dumpSQL(ctx context.Context, engine string, cfg Config, outPath string) error {
	switch engine {
	case EnginePostgres:
		cleanDSN, env := pgConn(cfg.DSN)
		_, err := sparkwing.Exec(ctx, "pg_dump", pgDumpArgs(cleanDSN, outPath, cfg.DumpArgs)...).EnvMap(env).Run()
		return err
	case EngineMySQL:
		conn, err := parseMySQLDSN(cfg.DSN)
		if err != nil {
			return err
		}
		args, env := mysqlDumpArgs(conn, outPath, cfg.DumpArgs)
		_, err = sparkwing.Exec(ctx, "mysqldump", args...).EnvMap(env).Run()
		return err
	}
	return fmt.Errorf("dbbackup: unsupported engine %q", engine)
}

func replaySQL(ctx context.Context, engine string, cfg Config, sqlPath string) error {
	switch engine {
	case EnginePostgres:
		cleanDSN, env := pgConn(cfg.DSN)
		args := pgRestoreArgs(cleanDSN, sqlPath, cfg.RestoreArgs)
		if dryRun() {
			sparkwing.Info(ctx, "[dry-run] would exec: %s", renderArgv("psql", args))
			return nil
		}
		_, err := sparkwing.Exec(ctx, "psql", args...).EnvMap(env).Run()
		return err
	case EngineMySQL:
		conn, err := parseMySQLDSN(cfg.DSN)
		if err != nil {
			return err
		}
		line, env := mysqlRestoreLine(conn, sqlPath, cfg.RestoreArgs)
		if dryRun() {
			sparkwing.Info(ctx, "[dry-run] would exec: %s", line)
			return nil
		}
		// safety: the mysql client reads the dump from stdin, so the
		// file is redirected in a bash line; conn carries no secret in
		// the line because the password travels via the MYSQL_PWD env.
		_, err = sparkwing.Bash(ctx, line).EnvMap(env).Run()
		return err
	}
	return fmt.Errorf("dbbackup: unsupported engine %q", engine)
}

func deliver(ctx context.Context, cfg Config, dest location, gzPath, name string) (string, error) {
	switch dest.scheme {
	case schemeLocal:
		if err := os.MkdirAll(cfg.Dest, 0o755); err != nil {
			return "", fmt.Errorf("dbbackup: create dest dir: %w", err)
		}
		finalPath := filepath.Join(cfg.Dest, name)
		if err := copyFile(gzPath, finalPath); err != nil {
			return "", fmt.Errorf("dbbackup: copy to dest: %w", err)
		}
		return finalPath, nil
	case schemeS3:
		uri := remoteObjectURI(cfg.Dest, name)
		if err := runCloud(ctx, "aws", s3UploadArgs(gzPath, uri, cfg.AWSProfile)...); err != nil {
			return "", err
		}
		return uri, nil
	case schemeGS:
		uri := remoteObjectURI(cfg.Dest, name)
		if err := runCloud(ctx, "gcloud", gsUploadArgs(gzPath, uri, cfg.Project)...); err != nil {
			return "", err
		}
		return uri, nil
	}
	return "", fmt.Errorf("dbbackup: unsupported destination scheme %q", dest.scheme)
}

func fetch(ctx context.Context, cfg Config, src location, workDir string) (string, func(), error) {
	noop := func() {}
	switch src.scheme {
	case schemeLocal:
		return cfg.Source, noop, nil
	case schemeS3:
		local := filepath.Join(workDir, "dbbackup-src-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql.gz")
		if err := execWithRetry(ctx, "aws", s3DownloadArgs(cfg.Source, local, cfg.AWSProfile)...); err != nil {
			return "", noop, err
		}
		return local, func() { _ = os.Remove(local) }, nil
	case schemeGS:
		local := filepath.Join(workDir, "dbbackup-src-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql.gz")
		if err := execWithRetry(ctx, "gcloud", gsDownloadArgs(cfg.Source, local, cfg.Project)...); err != nil {
			return "", noop, err
		}
		return local, func() { _ = os.Remove(local) }, nil
	}
	return "", noop, fmt.Errorf("dbbackup: unsupported source scheme %q", src.scheme)
}

func runCloud(ctx context.Context, name string, args ...string) error {
	if dryRun() {
		sparkwing.Info(ctx, "[dry-run] would exec: %s", renderArgv(name, args))
		return nil
	}
	return execWithRetry(ctx, name, args...)
}

const cloudExecRetries = 3

// execWithRetry runs an object-store transfer command with a bounded
// exponential backoff, so a single transient S3/GCS blip does not fail
// an entire backup or restore. It stops early when ctx is cancelled and
// returns the last error after the final attempt.
func execWithRetry(ctx context.Context, name string, args ...string) error {
	var err error
	for attempt := 1; attempt <= cloudExecRetries; attempt++ {
		if err = step.Exec(ctx, name, args...); err == nil {
			return nil
		}
		if attempt == cloudExecRetries {
			break
		}
		sparkwing.Warn(ctx, "%s failed (attempt %d/%d), retrying: %v", name, attempt, cloudExecRetries, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return err
}

func dryRun() bool { return os.Getenv("SPARKWING_DRY_RUN") != "" }

func renderArgv(name string, args []string) string {
	return strings.Join(append([]string{name}, args...), " ")
}

type scheme string

const (
	schemeLocal scheme = "local"
	schemeS3    scheme = "s3"
	schemeGS    scheme = "gs"
)

type location struct {
	scheme scheme
}

func classifyLocation(uri string) (location, error) {
	switch {
	case strings.HasPrefix(uri, "s3://"):
		return location{scheme: schemeS3}, nil
	case strings.HasPrefix(uri, "gs://"):
		return location{scheme: schemeGS}, nil
	case strings.Contains(uri, "://"):
		return location{}, fmt.Errorf("dbbackup: unsupported URL scheme in %q (want local path, s3://, or gs://)", uri)
	default:
		return location{scheme: schemeLocal}, nil
	}
}

func normalizeEngine(engine string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", EnginePostgres, "postgresql", "pg":
		return EnginePostgres, nil
	case EngineMySQL, "mariadb":
		return EngineMySQL, nil
	default:
		return "", fmt.Errorf("dbbackup: unsupported engine %q (want postgres or mysql)", engine)
	}
}

func dumpFilename(dbName string, now time.Time) string {
	if dbName == "" {
		dbName = "db"
	}
	return fmt.Sprintf("%s-%s.sql.gz", dbName, now.Format("20060102T150405Z"))
}

func dbNameFromDSN(engine, dsn string) string {
	if engine == EngineMySQL {
		if conn, err := parseMySQLDSN(dsn); err == nil && conn.DB != "" {
			return conn.DB
		}
		return "db"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "db"
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "db"
	}
	return name
}

func remoteObjectURI(prefix, name string) string {
	return strings.TrimRight(prefix, "/") + "/" + name
}

// safety: the password moves from the DSN into PGPASSWORD so it never lands
// on the argv the process table exposes.
func pgConn(dsn string) (string, map[string]string) {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn, nil
	}
	pw, ok := u.User.Password()
	if !ok {
		return dsn, nil
	}
	u.User = url.User(u.User.Username())
	return u.String(), map[string]string{"PGPASSWORD": pw}
}

func pgDumpArgs(dsn, outPath string, extra []string) []string {
	args := []string{"--dbname=" + dsn, "--no-owner", "--no-privileges", "--file=" + outPath}
	return append(args, extra...)
}

func pgRestoreArgs(dsn, sqlPath string, extra []string) []string {
	args := []string{"--dbname=" + dsn, "--set", "ON_ERROR_STOP=1", "--quiet", "--file=" + sqlPath}
	return append(args, extra...)
}

func pgVerifyArgs(dsn, query string) []string {
	return []string{"--dbname=" + dsn, "-tAc", query}
}

func s3UploadArgs(localPath, remoteURI, profile string) []string {
	args := []string{"s3", "cp", localPath, remoteURI}
	return append(args, aws.ProfileArgs(profile)...)
}

func s3DownloadArgs(remoteURI, localPath, profile string) []string {
	args := []string{"s3", "cp", remoteURI, localPath}
	return append(args, aws.ProfileArgs(profile)...)
}

func gsUploadArgs(localPath, remoteURI, project string) []string {
	args := []string{"storage", "cp", localPath, remoteURI}
	if project != "" {
		args = append(args, "--project", project)
	}
	return args
}

func gsDownloadArgs(remoteURI, localPath, project string) []string {
	args := []string{"storage", "cp", remoteURI, localPath}
	if project != "" {
		args = append(args, "--project", project)
	}
	return args
}

// conn is decomposed because the mysql client family, unlike libpq, does
// not accept a URL DSN.
type conn struct {
	Host     string
	Port     string
	User     string
	Password string
	DB       string
}

func parseMySQLDSN(dsn string) (conn, error) {
	var c conn
	if strings.HasPrefix(dsn, "mysql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return c, fmt.Errorf("dbbackup: parse mysql DSN: %w", err)
		}
		c.Host = u.Hostname()
		c.Port = u.Port()
		c.User = u.User.Username()
		c.Password, _ = u.User.Password()
		c.DB = strings.TrimPrefix(u.Path, "/")
	} else if strings.Contains(dsn, "@tcp(") {
		rest := dsn
		if at := strings.LastIndex(rest, "@tcp("); at >= 0 {
			cred := rest[:at]
			if colon := strings.IndexByte(cred, ':'); colon >= 0 {
				c.User = cred[:colon]
				c.Password = cred[colon+1:]
			} else {
				c.User = cred
			}
			rest = rest[at+len("@tcp("):]
		}
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return c, fmt.Errorf("dbbackup: malformed mysql DSN %q", dsn)
		}
		hostPort := rest[:end]
		if colon := strings.LastIndex(hostPort, ":"); colon >= 0 {
			c.Host = hostPort[:colon]
			c.Port = hostPort[colon+1:]
		} else {
			c.Host = hostPort
		}
		after := rest[end+1:]
		after = strings.TrimPrefix(after, "/")
		if q := strings.IndexByte(after, '?'); q >= 0 {
			after = after[:q]
		}
		c.DB = after
	} else {
		return c, fmt.Errorf("dbbackup: unrecognized mysql DSN %q (want mysql:// URL or user:pass@tcp(host:port)/db)", dsn)
	}
	if c.Host == "" {
		c.Host = "localhost"
	}
	if c.Port == "" {
		c.Port = "3306"
	}
	if c.DB == "" {
		return c, fmt.Errorf("dbbackup: mysql DSN %q has no database name", dsn)
	}
	return c, nil
}

func mysqlConnArgs(c conn) []string {
	return []string{"--host=" + c.Host, "--port=" + c.Port, "--user=" + c.User}
}

func mysqlEnv(c conn) map[string]string {
	if c.Password == "" {
		return nil
	}
	// safety: password is passed via MYSQL_PWD so it never lands on the
	// argv the process table exposes.
	return map[string]string{"MYSQL_PWD": c.Password}
}

// mysqlDumpArgs defaults to --single-transaction so an InnoDB dump is
// consistent without locking tables.
func mysqlDumpArgs(c conn, outPath string, extra []string) ([]string, map[string]string) {
	args := mysqlConnArgs(c)
	args = append(args, "--single-transaction", "--result-file="+outPath)
	args = append(args, extra...)
	args = append(args, c.DB)
	return args, mysqlEnv(c)
}

// safety: every interpolated field is shell-quoted so a value carrying a
// space or metacharacter cannot break the redirection or inject into the line.
func mysqlRestoreLine(c conn, sqlPath string, extra []string) (string, map[string]string) {
	parts := []string{"mysql"}
	for _, a := range mysqlConnArgs(c) {
		parts = append(parts, shellQuote(a))
	}
	for _, a := range extra {
		parts = append(parts, shellQuote(a))
	}
	parts = append(parts, shellQuote(c.DB), "<", shellQuote(sqlPath))
	return strings.Join(parts, " "), mysqlEnv(c)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func mysqlVerifyArgs(c conn, query string) ([]string, map[string]string) {
	args := mysqlConnArgs(c)
	args = append(args, "-N", "-B", "-e", query, c.DB)
	return args, mysqlEnv(c)
}

func parseRowCount(out string) (int, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty result")
	}
	return strconv.Atoi(fields[0])
}

func gzipFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	gw := gzip.NewWriter(out)
	if _, err := io.Copy(gw, in); err != nil {
		_ = gw.Close()
		return 0, err
	}
	if err := gw.Close(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	fi, err := os.Stat(dst)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

func gunzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	gr, err := gzip.NewReader(in)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, gr); err != nil {
		return err
	}
	return out.Close()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
