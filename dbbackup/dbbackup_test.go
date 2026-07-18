package dbbackup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeEngine(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", EnginePostgres, false},
		{"postgres", EnginePostgres, false},
		{"PostgreSQL", EnginePostgres, false},
		{"pg", EnginePostgres, false},
		{"mysql", EngineMySQL, false},
		{"MariaDB", EngineMySQL, false},
		{"  mysql  ", EngineMySQL, false},
		{"sqlite", "", true},
		{"mongo", "", true},
	}
	for _, tc := range cases {
		got, err := normalizeEngine(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("normalizeEngine(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("normalizeEngine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyLocation(t *testing.T) {
	cases := []struct {
		in      string
		want    scheme
		wantErr bool
	}{
		{"s3://bucket/prefix", schemeS3, false},
		{"gs://bucket/prefix", schemeGS, false},
		{"/var/backups", schemeLocal, false},
		{"backups", schemeLocal, false},
		{"./out", schemeLocal, false},
		{"http://example.com", "", true},
		{"azblob://x", "", true},
	}
	for _, tc := range cases {
		got, err := classifyLocation(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("classifyLocation(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got.scheme != tc.want {
			t.Errorf("classifyLocation(%q) = %q, want %q", tc.in, got.scheme, tc.want)
		}
	}
}

func TestDumpFilename(t *testing.T) {
	now := time.Date(2026, 7, 18, 4, 30, 15, 0, time.UTC)
	if got, want := dumpFilename("appdb", now), "appdb-20260718T043015Z.sql.gz"; got != want {
		t.Errorf("dumpFilename = %q, want %q", got, want)
	}
	if got, want := dumpFilename("", now), "db-20260718T043015Z.sql.gz"; got != want {
		t.Errorf("dumpFilename empty db = %q, want %q", got, want)
	}
}

func TestDBNameFromDSN(t *testing.T) {
	cases := []struct {
		engine string
		dsn    string
		want   string
	}{
		{EnginePostgres, "postgres://u:p@localhost:5432/appdb?sslmode=disable", "appdb"},
		{EnginePostgres, "postgres://u:p@localhost:5432/", "db"},
		{EnginePostgres, "postgresql://localhost/orders", "orders"},
		{EngineMySQL, "mysql://u:p@localhost:3306/shop", "shop"},
		{EngineMySQL, "u:p@tcp(127.0.0.1:3306)/inventory", "inventory"},
		{EngineMySQL, "garbage", "db"},
	}
	for _, tc := range cases {
		if got := dbNameFromDSN(tc.engine, tc.dsn); got != tc.want {
			t.Errorf("dbNameFromDSN(%q, %q) = %q, want %q", tc.engine, tc.dsn, got, tc.want)
		}
	}
}

func TestRemoteObjectURI(t *testing.T) {
	cases := []struct {
		prefix, name, want string
	}{
		{"s3://bucket/backups", "db-x.sql.gz", "s3://bucket/backups/db-x.sql.gz"},
		{"s3://bucket/backups/", "db-x.sql.gz", "s3://bucket/backups/db-x.sql.gz"},
		{"gs://bucket", "db-x.sql.gz", "gs://bucket/db-x.sql.gz"},
	}
	for _, tc := range cases {
		if got := remoteObjectURI(tc.prefix, tc.name); got != tc.want {
			t.Errorf("remoteObjectURI(%q, %q) = %q, want %q", tc.prefix, tc.name, got, tc.want)
		}
	}
}

func TestPgArgs(t *testing.T) {
	dsn := "postgres://u:p@localhost/appdb"
	if got, want := strings.Join(pgDumpArgs(dsn, "/tmp/x.sql", nil), " "),
		"--dbname=postgres://u:p@localhost/appdb --no-owner --no-privileges --file=/tmp/x.sql"; got != want {
		t.Errorf("pgDumpArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(pgDumpArgs(dsn, "/tmp/x.sql", []string{"--schema-only"}), " "),
		"--dbname=postgres://u:p@localhost/appdb --no-owner --no-privileges --file=/tmp/x.sql --schema-only"; got != want {
		t.Errorf("pgDumpArgs passthrough = %q, want %q", got, want)
	}
	if got, want := strings.Join(pgRestoreArgs(dsn, "/tmp/x.sql", nil), " "),
		"--dbname=postgres://u:p@localhost/appdb --set ON_ERROR_STOP=1 --quiet --file=/tmp/x.sql"; got != want {
		t.Errorf("pgRestoreArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(pgVerifyArgs(dsn, "SELECT count(*) FROM t"), "|"),
		"--dbname=postgres://u:p@localhost/appdb|-tAc|SELECT count(*) FROM t"; got != want {
		t.Errorf("pgVerifyArgs = %q, want %q", got, want)
	}
}

func TestPgConn_StripsPasswordToEnv(t *testing.T) {
	clean, env := pgConn("postgres://app:s3cr3t@db.internal:5432/appdb?sslmode=require")
	if strings.Contains(clean, "s3cr3t") {
		t.Errorf("pgConn left password on DSN: %q", clean)
	}
	if env["PGPASSWORD"] != "s3cr3t" {
		t.Errorf("pgConn PGPASSWORD = %q, want s3cr3t", env["PGPASSWORD"])
	}
	if !strings.Contains(clean, "app@db.internal:5432/appdb") {
		t.Errorf("pgConn dropped connection detail: %q", clean)
	}
	clean, env = pgConn("postgres://app@db.internal/appdb")
	if env != nil {
		t.Errorf("pgConn with no password env = %v, want nil", env)
	}
	if clean != "postgres://app@db.internal/appdb" {
		t.Errorf("pgConn passwordless DSN mutated: %q", clean)
	}
}

func TestS3AndGSArgs(t *testing.T) {
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_PROFILE", "")
	if got, want := strings.Join(s3UploadArgs("/tmp/d.sql.gz", "s3://b/k.sql.gz", "prod"), " "),
		"s3 cp /tmp/d.sql.gz s3://b/k.sql.gz --profile prod"; got != want {
		t.Errorf("s3UploadArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(s3DownloadArgs("s3://b/k.sql.gz", "/tmp/d.sql.gz", "prod"), " "),
		"s3 cp s3://b/k.sql.gz /tmp/d.sql.gz --profile prod"; got != want {
		t.Errorf("s3DownloadArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(gsUploadArgs("/tmp/d.sql.gz", "gs://b/k.sql.gz", "proj"), " "),
		"storage cp /tmp/d.sql.gz gs://b/k.sql.gz --project proj"; got != want {
		t.Errorf("gsUploadArgs = %q, want %q", got, want)
	}
	if got, want := strings.Join(gsUploadArgs("/tmp/d.sql.gz", "gs://b/k.sql.gz", ""), " "),
		"storage cp /tmp/d.sql.gz gs://b/k.sql.gz"; got != want {
		t.Errorf("gsUploadArgs no project = %q, want %q", got, want)
	}
}

func TestS3UploadArgs_IRSAOmitsProfile(t *testing.T) {
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/secrets/token")
	if got, want := strings.Join(s3UploadArgs("/tmp/d.sql.gz", "s3://b/k.sql.gz", "prod"), " "),
		"s3 cp /tmp/d.sql.gz s3://b/k.sql.gz"; got != want {
		t.Errorf("s3UploadArgs under IRSA = %q, want %q", got, want)
	}
}

func TestParseMySQLDSN(t *testing.T) {
	cases := []struct {
		in      string
		want    conn
		wantErr bool
	}{
		{
			in:   "mysql://app:secret@db.internal:3307/shop",
			want: conn{Host: "db.internal", Port: "3307", User: "app", Password: "secret", DB: "shop"},
		},
		{
			in:   "mysql://app@db.internal/shop",
			want: conn{Host: "db.internal", Port: "3306", User: "app", Password: "", DB: "shop"},
		},
		{
			in:   "app:secret@tcp(127.0.0.1:3306)/inventory?parseTime=true",
			want: conn{Host: "127.0.0.1", Port: "3306", User: "app", Password: "secret", DB: "inventory"},
		},
		{
			in:   "root@tcp(localhost)/appdb",
			want: conn{Host: "localhost", Port: "3306", User: "root", Password: "", DB: "appdb"},
		},
		{in: "mysql://app:secret@host:3306/", wantErr: true},
		{in: "not-a-dsn", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseMySQLDSN(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseMySQLDSN(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("parseMySQLDSN(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestMySQLArgs(t *testing.T) {
	c := conn{Host: "h", Port: "3306", User: "u", Password: "pw", DB: "d"}
	dumpArgs, dumpEnv := mysqlDumpArgs(c, "/tmp/d.sql", nil)
	if got, want := strings.Join(dumpArgs, " "), "--host=h --port=3306 --user=u --single-transaction --result-file=/tmp/d.sql d"; got != want {
		t.Errorf("mysqlDumpArgs = %q, want %q", got, want)
	}
	if dumpEnv["MYSQL_PWD"] != "pw" {
		t.Errorf("mysqlDumpArgs env MYSQL_PWD = %q, want pw", dumpEnv["MYSQL_PWD"])
	}
	passArgs, _ := mysqlDumpArgs(c, "/tmp/d.sql", []string{"--no-data"})
	if got, want := strings.Join(passArgs, " "),
		"--host=h --port=3306 --user=u --single-transaction --result-file=/tmp/d.sql --no-data d"; got != want {
		t.Errorf("mysqlDumpArgs passthrough = %q, want %q", got, want)
	}
	line, _ := mysqlRestoreLine(c, "/tmp/d.sql", nil)
	if want := "mysql '--host=h' '--port=3306' '--user=u' 'd' < '/tmp/d.sql'"; line != want {
		t.Errorf("mysqlRestoreLine = %q, want %q", line, want)
	}
	if got, _ := mysqlRestoreLine(conn{Host: "h", Port: "3306", User: "u", DB: "d b"}, "/tmp/d.sql", nil); !strings.Contains(got, "'d b'") {
		t.Errorf("mysqlRestoreLine did not quote a db name with a space: %q", got)
	}
	verifyArgs, _ := mysqlVerifyArgs(c, "SELECT 1")
	if got, want := strings.Join(verifyArgs, "|"), "--host=h|--port=3306|--user=u|-N|-B|-e|SELECT 1|d"; got != want {
		t.Errorf("mysqlVerifyArgs = %q, want %q", got, want)
	}
}

func TestMySQLEnv_NoPasswordOmitsEnv(t *testing.T) {
	c := conn{Host: "h", Port: "3306", User: "u", DB: "d"}
	if env := mysqlEnv(c); env != nil {
		t.Errorf("mysqlEnv with no password = %v, want nil", env)
	}
}

func TestParseRowCount(t *testing.T) {
	cases := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"42\n", 42, false},
		{"  7 ", 7, false},
		{"12\n(1 row)", 12, false},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := parseRowCount(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseRowCount(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("parseRowCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRenderArgv(t *testing.T) {
	if got, want := renderArgv("aws", []string{"s3", "cp", "/tmp/x", "s3://b/x"}),
		"aws s3 cp /tmp/x s3://b/x"; got != want {
		t.Errorf("renderArgv = %q, want %q", got, want)
	}
	if got, want := renderArgv("gcloud", nil), "gcloud"; got != want {
		t.Errorf("renderArgv no args = %q, want %q", got, want)
	}
}

func TestDryRun_ReadsEnv(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	if dryRun() {
		t.Error("dryRun with empty env should be false")
	}
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if !dryRun() {
		t.Error("dryRun with non-empty env should be true")
	}
}

func TestGzipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "dump.sql")
	payload := bytes.Repeat([]byte("CREATE TABLE t (id int);\n"), 500)
	if err := os.WriteFile(srcPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	gzPath := filepath.Join(dir, "dump.sql.gz")
	size, err := gzipFile(srcPath, gzPath)
	if err != nil {
		t.Fatalf("gzipFile: %v", err)
	}
	if size <= 0 {
		t.Fatalf("gzipFile size = %d, want > 0", size)
	}
	if fi, _ := os.Stat(gzPath); fi == nil || fi.Size() != size {
		t.Fatalf("reported size %d does not match file", size)
	}
	outPath := filepath.Join(dir, "restored.sql")
	if err := gunzipFile(gzPath, outPath); err != nil {
		t.Fatalf("gunzipFile: %v", err)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("round-trip payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.gz")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "sub", "b.gz")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "hello" {
		t.Errorf("copyFile content = %q, want hello", got)
	}
}

func TestDump_ValidationErrors(t *testing.T) {
	ctx := t.Context()
	if _, err := Dump(ctx, Config{Engine: "mongo", DSN: "x", Dest: "/tmp"}); err == nil {
		t.Error("expected error for bad engine")
	}
	if _, err := Dump(ctx, Config{DSN: "", Dest: "/tmp"}); err == nil {
		t.Error("expected error for missing DSN")
	}
	if _, err := Dump(ctx, Config{DSN: "postgres://x", Dest: ""}); err == nil {
		t.Error("expected error for missing Dest")
	}
	if _, err := Dump(ctx, Config{DSN: "postgres://x", Dest: "http://nope"}); err == nil {
		t.Error("expected error for unsupported dest scheme")
	}
}

func TestRestore_ValidationErrors(t *testing.T) {
	ctx := t.Context()
	if err := Restore(ctx, Config{Engine: "mongo", DSN: "x", Source: "/tmp/x.gz"}); err == nil {
		t.Error("expected error for bad engine")
	}
	if err := Restore(ctx, Config{DSN: "", Source: "/tmp/x.gz"}); err == nil {
		t.Error("expected error for missing DSN")
	}
	if err := Restore(ctx, Config{DSN: "postgres://x", Source: ""}); err == nil {
		t.Error("expected error for missing Source")
	}
}

func TestVerifyRestore_ValidationErrors(t *testing.T) {
	ctx := t.Context()
	if err := VerifyRestore(ctx, VerifyConfig{Engine: "mongo", DSN: "x"}); err == nil {
		t.Error("expected error for bad engine")
	}
	if err := VerifyRestore(ctx, VerifyConfig{DSN: ""}); err == nil {
		t.Error("expected error for missing DSN")
	}
}
