package s3

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/sparkwing-dev/sparks-core/aws"
)

func awsEnvOff(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_PROFILE", "")
}

func TestAssetSyncArgs_ProfileAndDelete(t *testing.T) {
	awsEnvOff(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out", AWSProfile: "ci", Delete: true}
	got := assetSyncArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), nil)
	want := []string{
		"s3", "sync", "out/", "s3://site",
		"--profile", "ci",
		"--delete",
		"--cache-control", "public, max-age=31536000, immutable",
		"--exclude", "*.html",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("assetSyncArgs = %v, want %v", got, want)
	}
}

func TestAssetSyncArgs_EmptyProfileOmitsFlag(t *testing.T) {
	awsEnvOff(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out"}
	got := assetSyncArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), nil)
	if joined := strings.Join(got, " "); strings.Contains(joined, "--profile") {
		t.Fatalf("assetSyncArgs with empty profile carries --profile: %v", got)
	}
}

// No caller profile means no --profile, even with AWS_PROFILE set, so
// the aws CLI applies its own precedence rather than having an
// inherited variable promoted into an override.
func TestAssetSyncArgs_NoProfileLeavesAWSProfileEnvToTheCLI(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_PROFILE", "from-env")
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out"}
	got := assetSyncArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), nil)
	if joined := strings.Join(got, " "); strings.Contains(joined, "--profile") {
		t.Fatalf("assetSyncArgs passed a profile with none configured: %v", got)
	}
}

func TestAssetSyncArgs_IRSADropsProfile(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out", AWSProfile: "ci"}
	got := assetSyncArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), nil)
	if joined := strings.Join(got, " "); strings.Contains(joined, "--profile") {
		t.Fatalf("assetSyncArgs under IRSA carries --profile: %v", got)
	}
}

func TestHTMLCopyArgs_NoCacheHeadersAndHTMLOnly(t *testing.T) {
	awsEnvOff(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out", AWSProfile: "ci"}
	got := htmlCopyArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), []string{"--exclude", "releases/*"})
	want := []string{
		"s3", "cp", "out/", "s3://site",
		"--profile", "ci",
		"--recursive",
		"--cache-control", "no-cache, no-store, must-revalidate",
		"--exclude", "*",
		"--include", "*.html",
		"--exclude", "releases/*",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("htmlCopyArgs = %v, want %v", got, want)
	}
}

func TestHTMLOrphanSyncArgs_DeleteScopedToHTML(t *testing.T) {
	awsEnvOff(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: "out"}
	got := htmlOrphanSyncArgs(cfg, aws.ProfileArgs(cfg.AWSProfile), []string{"--exclude", "releases/*"})
	want := []string{
		"s3", "sync", "out/", "s3://site",
		"--delete",
		"--exclude", "*",
		"--include", "*.html",
		"--exclude", "releases/*",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("htmlOrphanSyncArgs = %v, want %v", got, want)
	}
}

// fakeAWSCLI puts a stub named "aws" first on PATH that appends one
// line per argument, then a "---" terminator, to a log file, and
// returns that log path. It lets a test read back exactly what
// DeployStaticSite asked the aws CLI to do without touching S3, which
// is the only way to pin the config-to-argv wiring inside the block
// itself rather than re-deriving it in the test.
func fakeAWSCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	stub := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" >> " + strconv.Quote(logPath) + "\n" +
		"printf '%s\\n' '---' >> " + strconv.Quote(logPath) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "aws"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write aws stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// recordedInvocations reads a fakeAWSCLI log back as one argv slice per
// aws invocation, in call order.
func recordedInvocations(t *testing.T, logPath string) [][]string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read aws stub log: %v", err)
	}
	var runs [][]string
	var cur []string
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "---" {
			runs = append(runs, cur)
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return runs
}

// siteDir builds a minimal build output directory: one asset, one HTML.
func siteDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "<html></html>", "app.js": "console.log(1)"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// hasFlagValue reports whether argv contains flag immediately followed
// by value.
func hasFlagValue(argv []string, flag, value string) bool {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) && argv[i+1] == value {
			return true
		}
	}
	return false
}

func TestDeployStaticSite_EmptyProfileSendsNoProfileFlag(t *testing.T) {
	awsEnvOff(t)
	logPath := fakeAWSCLI(t)
	if _, err := DeployStaticSite(context.Background(), StaticSiteConfig{Bucket: "site", OutDir: siteDir(t)}); err != nil {
		t.Fatalf("DeployStaticSite with an empty AWSProfile: %v", err)
	}
	runs := recordedInvocations(t, logPath)
	if len(runs) != 2 {
		t.Fatalf("aws invocations = %d, want 2 (asset sync, html cp)", len(runs))
	}
	for _, argv := range runs {
		for _, a := range argv {
			if a == "--profile" {
				t.Fatalf("empty AWSProfile still sent --profile: %v", argv)
			}
		}
	}
}

func TestDeployStaticSite_ConfiguredProfileReachesTheCLI(t *testing.T) {
	awsEnvOff(t)
	logPath := fakeAWSCLI(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: siteDir(t), AWSProfile: "ci"}
	if _, err := DeployStaticSite(context.Background(), cfg); err != nil {
		t.Fatalf("DeployStaticSite: %v", err)
	}
	runs := recordedInvocations(t, logPath)
	if len(runs) != 2 {
		t.Fatalf("aws invocations = %d, want 2 (asset sync, html cp)", len(runs))
	}
	for _, argv := range runs {
		if !hasFlagValue(argv, "--profile", "ci") {
			t.Fatalf("configured profile missing from argv: %v", argv)
		}
	}
}

func TestDeployStaticSite_CallerProfileBeatsAWSProfileEnv(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_PROFILE", "from-env")
	logPath := fakeAWSCLI(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: siteDir(t), AWSProfile: "caller"}
	if _, err := DeployStaticSite(context.Background(), cfg); err != nil {
		t.Fatalf("DeployStaticSite: %v", err)
	}
	for _, argv := range recordedInvocations(t, logPath) {
		if hasFlagValue(argv, "--profile", "from-env") {
			t.Fatalf("AWS_PROFILE overrode the caller: %v", argv)
		}
		if !hasFlagValue(argv, "--profile", "caller") {
			t.Fatalf("caller profile missing from argv: %v", argv)
		}
	}
}

func TestDeployStaticSite_DryRunWritesNothing(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("SPARKWING_DRY_RUN", "1")
	logPath := fakeAWSCLI(t)
	if _, err := DeployStaticSite(context.Background(), StaticSiteConfig{Bucket: "site", OutDir: siteDir(t), Delete: true}); err != nil {
		t.Fatalf("DeployStaticSite: %v", err)
	}
	// The stub only creates its log when it is invoked, so an absent
	// file is the assertion: the CLI never ran.
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("dry run still ran the aws CLI: %v", recordedInvocations(t, logPath))
	}
}

func TestDeployStaticSite_WrongAccountRefuses(t *testing.T) {
	awsEnvOff(t)
	fakeAWSCLI(t)
	cfg := StaticSiteConfig{Bucket: "site", OutDir: siteDir(t), ExpectedAccountID: "111111111111"}
	_, err := DeployStaticSite(context.Background(), cfg)
	if err == nil {
		t.Fatal("DeployStaticSite deployed to an account the caller did not name")
	}
	if !strings.Contains(err.Error(), "refusing to deploy") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeployStaticSite_MissingBucketRejected(t *testing.T) {
	awsEnvOff(t)
	fakeAWSCLI(t)
	_, err := DeployStaticSite(context.Background(), StaticSiteConfig{OutDir: siteDir(t)})
	if err == nil {
		t.Fatal("DeployStaticSite with no Bucket returned nil error")
	}
	if !strings.Contains(err.Error(), "bucket required") {
		t.Fatalf("DeployStaticSite error = %v, want a bucket-required error", err)
	}
}

func TestCountUploads(t *testing.T) {
	out := "upload: out/a.js to s3://site/a.js\nCompleted 1 file(s)\nupload: out/b.html to s3://site/b.html\n"
	if got := countUploads(out); got != 2 {
		t.Fatalf("countUploads = %d, want 2", got)
	}
	if got := countUploads(""); got != 0 {
		t.Fatalf("countUploads(empty) = %d, want 0", got)
	}
}
