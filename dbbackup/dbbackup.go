// Package dbbackup dumps a database to a compressed artifact, ships it
// to an object store, restores it into a target database, and verifies
// the restore. It is the shared block behind the scheduled-db-backup
// (dump + upload) and db-backup-restore-drill (dump + restore + verify)
// templates.
//
// Two databases engines are supported, selected by Config.Engine:
// "postgres" (pg_dump / psql) and "mysql" (mysqldump / mysql). The
// dump is written as plain SQL and gzip-compressed in-process, so the
// artifact is always a `<db>-<timestamp>.sql.gz` object regardless of
// engine.
//
// Destinations are chosen by URL scheme on Config.Dest (and the source
// scheme on Config.Source for Restore):
//
//   - a local directory: the artifact is copied there.
//   - s3://bucket/prefix: uploaded with `aws s3 cp` (see the aws module
//     for profile / IRSA resolution).
//   - gs://bucket/prefix: uploaded with `gcloud storage cp`.
//
// Both entry points return a func(ctx) error-shaped unit of work (Dump
// additionally hands back an Artifact handle), so they drop straight
// into a sparkwing Job body, a Job.Verify, or an OnFailure recovery.
// RestoreFunc turns a prior Dump's Artifact into an OnFailure-shaped
// rollback closure, which is the snapshot-then-migrate safety net.
//
// # Dry-run
//
// Cloud-mutating operations (the s3:// and gs:// uploads) honor the
// SPARKWING_DRY_RUN environment variable: when it is non-empty they
// echo the exact command argv they would run and return success without
// executing. The local dump, the local restore, and cloud downloads
// read or produce local state and always run for real.
//
// # Required host binaries
//
// pg_dump and psql for postgres; mysqldump and mysql for mysql; the
// `aws` CLI for s3:// destinations; the `gcloud` CLI for gs://
// destinations.
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

// Engine identifiers accepted by Config.Engine.
const (
	EnginePostgres = "postgres"
	EngineMySQL    = "mysql"
)

// Config drives Dump and Restore. A zero Engine resolves to postgres.
type Config struct {
	// Engine selects the database toolchain: "postgres" (default) or
	// "mysql".
	Engine string
	// DSN is the database connection string. It is the source database
	// for Dump and the target database for Restore. Postgres accepts a
	// libpq URI or key=value string verbatim; mysql accepts a
	// mysql://user:pass@host:port/db URL or the go-sql-driver
	// user:pass@tcp(host:port)/db form.
	DSN string
	// Dest is the Dump destination: a local directory, s3://bucket/prefix,
	// or gs://bucket/prefix. Required by Dump, ignored by Restore.
	Dest string
	// Source is the Restore source: a local .sql.gz path, or an
	// s3:///gs:// object URL. Required by Restore, ignored by Dump.
	Source string
	// AWSProfile is the profile for s3:// destinations. Empty uses the
	// runner's default credential chain (or IRSA on EKS). Ignored for
	// gs:// and local.
	AWSProfile string
	// Project is the GCP project for gs:// destinations. Empty omits the
	// --project flag. Ignored for s3:// and local.
	Project string
	// Filename overrides the generated `<db>-<timestamp>.sql.gz`
	// artifact basename.
	Filename string
	// WorkDir is the local scratch directory for the intermediate dump.
	// Defaults to the OS temp dir.
	WorkDir string
}

// Artifact is a handle to a produced backup: the final location (local
// path, s3://, or gs:// URL) and its compressed size in bytes. Dump
// returns it so a caller can wire the URI as a later Restore's Source
// (the returned prior-state handle for a restore drill).
type Artifact struct {
	URI   string
	Bytes int64
}

func (c *Config) engine() string {
	if c.Engine == "" {
		return EnginePostgres
	}
	return c.Engine
}

// Dump runs pg_dump/mysqldump against Config.DSN, gzip-compresses the
// SQL into a `<db>-<timestamp>.sql.gz` artifact, and delivers it to
// Config.Dest (a local directory, or an s3:// / gs:// prefix). The
// upload honors SPARKWING_DRY_RUN; the dump itself always runs. The
// returned Artifact carries the final URI and compressed size.
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
		sqlPath := filepath.Join(workDir, strings.TrimSuffix(name, ".gz"))
		gzPath := filepath.Join(workDir, name)
		if err := dumpSQL(ctx, engine, cfg, sqlPath); err != nil {
			return err
		}
		defer os.Remove(sqlPath)
		size, err := gzipFile(sqlPath, gzPath)
		if err != nil {
			return fmt.Errorf("dbbackup: compress dump: %w", err)
		}
		art.Bytes = size
		art.URI, err = deliver(ctx, cfg, dest, gzPath, name)
		if err != nil {
			return err
		}
		sparkwing.Info(ctx, "dumped %s (%d bytes) -> %s", engine, size, art.URI)
		return nil
	})
	return art, err
}

// Restore pulls Config.Source (a local .sql.gz, or an s3:// / gs://
// object), decompresses it, and replays it into Config.DSN with psql
// (postgres) or the mysql client (mysql). It is shaped as func(ctx)
// error for a Job body or an OnFailure handler. Restoring into a target
// database is a local mutation and always runs; a cloud download reads
// state and also always runs.
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
		defer os.Remove(sqlPath)
		if err := replaySQL(ctx, engine, cfg, sqlPath); err != nil {
			return err
		}
		sparkwing.Info(ctx, "restored %s into target database", engine)
		return nil
	})
}

// RestoreFunc returns a func(ctx) error that restores art back into dsn.
// It is meant as a Job OnFailure handler after a snapshot Dump: dump
// before a risky migration, wire the returned closure as OnFailure, and
// a failed migration rolls the database back to the snapshot.
func RestoreFunc(art Artifact, engine, dsn string) func(context.Context) error {
	return func(ctx context.Context) error {
		return Restore(ctx, Config{Engine: engine, DSN: dsn, Source: art.URI})
	}
}

// VerifyConfig configures a restore verification query.
type VerifyConfig struct {
	// Engine selects the client: "postgres" (default) or "mysql".
	Engine string
	// DSN is the database to query (typically the just-restored target).
	DSN string
	// Query is the SQL to run. Defaults to "SELECT 1".
	Query string
	// MinRows, when > 0, requires the first cell of the first result row
	// to parse as an integer >= MinRows, turning "SELECT count(*) FROM t"
	// into a row-count assertion. When 0, any error-free result passes.
	MinRows int
}

func (c *VerifyConfig) engine() string {
	if c.Engine == "" {
		return EnginePostgres
	}
	return c.Engine
}

// VerifyRestore runs VerifyConfig.Query against the database and reports
// whether the restore looks healthy. With MinRows == 0 it passes when
// the query returns without error (a smoke check). With MinRows > 0 it
// parses the first cell of the first row as an integer and fails unless
// it is at least MinRows. It reads state, so it always runs. Use it as a
// Job.Verify or a drill assertion.
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
			out, err = sparkwing.Exec(ctx, "psql", pgVerifyArgs(cfg.DSN, query)...).String()
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

// dumpSQL shells out to pg_dump/mysqldump writing plain SQL to outPath.
func dumpSQL(ctx context.Context, engine string, cfg Config, outPath string) error {
	switch engine {
	case EnginePostgres:
		return step.Exec(ctx, "pg_dump", pgDumpArgs(cfg.DSN, outPath)...)
	case EngineMySQL:
		conn, err := parseMySQLDSN(cfg.DSN)
		if err != nil {
			return err
		}
		args, env := mysqlDumpArgs(conn, outPath)
		_, err = sparkwing.Exec(ctx, "mysqldump", args...).EnvMap(env).Run()
		return err
	}
	return fmt.Errorf("dbbackup: unsupported engine %q", engine)
}

// replaySQL feeds a plain-SQL file into the target database.
func replaySQL(ctx context.Context, engine string, cfg Config, sqlPath string) error {
	switch engine {
	case EnginePostgres:
		return step.Exec(ctx, "psql", pgRestoreArgs(cfg.DSN, sqlPath)...)
	case EngineMySQL:
		conn, err := parseMySQLDSN(cfg.DSN)
		if err != nil {
			return err
		}
		line, env := mysqlRestoreLine(conn, sqlPath)
		// safety: the mysql client reads the dump from stdin, so the
		// file is redirected in a bash line; conn carries no secret in
		// the line because the password travels via the MYSQL_PWD env.
		_, err = sparkwing.Bash(ctx, line).EnvMap(env).Run()
		return err
	}
	return fmt.Errorf("dbbackup: unsupported engine %q", engine)
}

// deliver places the local gz artifact at the destination and returns
// its final URI. Cloud uploads honor SPARKWING_DRY_RUN.
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

// fetch resolves the Restore source to a local gz path, downloading from
// s3:///gs:// when needed. The returned cleanup removes any temp file it
// created (a no-op for a local source).
func fetch(ctx context.Context, cfg Config, src location, workDir string) (string, func(), error) {
	noop := func() {}
	switch src.scheme {
	case schemeLocal:
		return cfg.Source, noop, nil
	case schemeS3:
		local := filepath.Join(workDir, "dbbackup-src-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql.gz")
		if err := step.Exec(ctx, "aws", s3DownloadArgs(cfg.Source, local, cfg.AWSProfile)...); err != nil {
			return "", noop, err
		}
		return local, func() { os.Remove(local) }, nil
	case schemeGS:
		local := filepath.Join(workDir, "dbbackup-src-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sql.gz")
		if err := step.Exec(ctx, "gcloud", gsDownloadArgs(cfg.Source, local, cfg.Project)...); err != nil {
			return "", noop, err
		}
		return local, func() { os.Remove(local) }, nil
	}
	return "", noop, fmt.Errorf("dbbackup: unsupported source scheme %q", src.scheme)
}

// runCloud runs a cloud-mutating command, or under SPARKWING_DRY_RUN
// echoes the exact argv it would run and returns nil without executing.
func runCloud(ctx context.Context, name string, args ...string) error {
	if dryRun() {
		sparkwing.Info(ctx, "[dry-run] would exec: %s", renderArgv(name, args))
		return nil
	}
	return step.Exec(ctx, name, args...)
}

func dryRun() bool { return os.Getenv("SPARKWING_DRY_RUN") != "" }

// renderArgv joins a command and its arguments into a single inspectable
// line for the dry-run echo.
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

// classifyLocation determines whether a dest/source URI is local,
// s3://, or gs://.
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

// normalizeEngine canonicalizes an engine string, accepting a few
// common aliases, and rejects anything unsupported.
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

// dumpFilename builds the timestamped `<db>-<timestamp>.sql.gz` object
// basename.
func dumpFilename(dbName string, now time.Time) string {
	if dbName == "" {
		dbName = "db"
	}
	return fmt.Sprintf("%s-%s.sql.gz", dbName, now.Format("20060102T150405Z"))
}

// dbNameFromDSN extracts a database name from a DSN for the artifact
// filename, defaulting to "db" when it can't be parsed.
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

// remoteObjectURI joins a bucket prefix and object name into a single
// s3:///gs:// object URI.
func remoteObjectURI(prefix, name string) string {
	return strings.TrimRight(prefix, "/") + "/" + name
}

// pgDumpArgs builds the pg_dump argv for a plain-SQL dump to outPath.
func pgDumpArgs(dsn, outPath string) []string {
	return []string{"--dbname=" + dsn, "--no-owner", "--no-privileges", "--file=" + outPath}
}

// pgRestoreArgs builds the psql argv to replay a plain-SQL file. It
// stops on the first error so a broken dump fails the restore.
func pgRestoreArgs(dsn, sqlPath string) []string {
	return []string{"--dbname=" + dsn, "--set", "ON_ERROR_STOP=1", "--quiet", "--file=" + sqlPath}
}

// pgVerifyArgs builds the psql argv to run a single verification query
// returning an unaligned, tuples-only result.
func pgVerifyArgs(dsn, query string) []string {
	return []string{"--dbname=" + dsn, "-tAc", query}
}

// s3UploadArgs builds the `aws s3 cp` argv for a single-object upload.
func s3UploadArgs(localPath, remoteURI, profile string) []string {
	args := []string{"s3", "cp", localPath, remoteURI}
	return append(args, aws.ProfileArgs(profile)...)
}

// s3DownloadArgs builds the `aws s3 cp` argv to fetch a single object.
func s3DownloadArgs(remoteURI, localPath, profile string) []string {
	args := []string{"s3", "cp", remoteURI, localPath}
	return append(args, aws.ProfileArgs(profile)...)
}

// gsUploadArgs builds the `gcloud storage cp` argv for a single-object
// upload, adding --project only when project is set.
func gsUploadArgs(localPath, remoteURI, project string) []string {
	args := []string{"storage", "cp", localPath, remoteURI}
	if project != "" {
		args = append(args, "--project", project)
	}
	return args
}

// gsDownloadArgs builds the `gcloud storage cp` argv to fetch a single
// object.
func gsDownloadArgs(remoteURI, localPath, project string) []string {
	args := []string{"storage", "cp", remoteURI, localPath}
	if project != "" {
		args = append(args, "--project", project)
	}
	return args
}

// conn holds decomposed connection parameters for the mysql client
// family, which (unlike libpq) does not accept a URL DSN.
type conn struct {
	Host     string
	Port     string
	User     string
	Password string
	DB       string
}

// parseMySQLDSN parses a mysql://user:pass@host:port/db URL or the
// go-sql-driver user:pass@tcp(host:port)/db form into connection parts.
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

// mysqlConnArgs builds the shared host/port/user connection flags.
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

// mysqlDumpArgs builds the mysqldump argv (and env) for a plain-SQL dump
// to outPath.
func mysqlDumpArgs(c conn, outPath string) ([]string, map[string]string) {
	args := mysqlConnArgs(c)
	args = append(args, "--result-file="+outPath, c.DB)
	return args, mysqlEnv(c)
}

// mysqlRestoreLine builds the bash line (and env) to replay a plain-SQL
// file into the target via stdin redirection.
func mysqlRestoreLine(c conn, sqlPath string) (string, map[string]string) {
	line := strings.Join(mysqlConnArgs(c), " ")
	line = "mysql " + line + " " + c.DB + " < " + sqlPath
	return line, mysqlEnv(c)
}

// mysqlVerifyArgs builds the mysql client argv (and env) to run a single
// query in batch mode without column headers.
func mysqlVerifyArgs(c conn, query string) ([]string, map[string]string) {
	args := mysqlConnArgs(c)
	args = append(args, "-N", "-B", "-e", query, c.DB)
	return args, mysqlEnv(c)
}

// parseRowCount reads the first whitespace-delimited token of query
// output as an integer.
func parseRowCount(out string) (int, error) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, fmt.Errorf("empty result")
	}
	return strconv.Atoi(fields[0])
}

// gzipFile gzip-compresses src into dst and returns dst's size.
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
		gw.Close()
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

// gunzipFile decompresses a gzip src into dst.
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
	defer gr.Close()
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
