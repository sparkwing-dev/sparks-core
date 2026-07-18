package release

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func TestArtifactPath(t *testing.T) {
	cases := []struct {
		goos, goarch string
		want         string
	}{
		{"linux", "amd64", "dist/app_1.2.3_linux_amd64"},
		{"darwin", "arm64", "dist/app_1.2.3_darwin_arm64"},
		{"windows", "amd64", "dist/app_1.2.3_windows_amd64.exe"},
	}
	for _, tc := range cases {
		got := ArtifactPath("dist", "app", "1.2.3", tc.goos, tc.goarch)
		if got != filepath.FromSlash(tc.want) {
			t.Errorf("ArtifactPath(%s/%s) = %q, want %q", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestBuildArgs(t *testing.T) {
	got := buildArgs(CrossBuildConfig{MainPkg: "./cmd/app"}, "dist/app_1.0.0_linux_amd64")
	want := []string{"build", "-o", "dist/app_1.0.0_linux_amd64", "./cmd/app"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestBuildArgs_WithLDFlags(t *testing.T) {
	got := buildArgs(CrossBuildConfig{MainPkg: ".", LDFlags: "-s -w"}, "out")
	want := []string{"build", "-o", "out", "-ldflags", "-s -w", "."}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestBuildArgs_TrimpathTagsAndExtraFlags(t *testing.T) {
	got := buildArgs(CrossBuildConfig{
		MainPkg:    "./cmd/app",
		LDFlags:    "-s",
		Trimpath:   true,
		Tags:       []string{"netgo", "osusergo"},
		BuildFlags: []string{"-mod=vendor"},
	}, "out")
	want := []string{"build", "-o", "out", "-trimpath", "-tags", "netgo,osusergo", "-ldflags", "-s", "-mod=vendor", "./cmd/app"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("buildArgs = %v, want %v", got, want)
	}
}

func TestSplitPlatform(t *testing.T) {
	goos, goarch, err := splitPlatform("linux/arm64")
	if err != nil {
		t.Fatalf("splitPlatform: %v", err)
	}
	if goos != "linux" || goarch != "arm64" {
		t.Errorf("got %s/%s, want linux/arm64", goos, goarch)
	}
	for _, bad := range []string{"linux", "linux/", "/amd64", "a/b/c", ""} {
		if _, _, err := splitPlatform(bad); err == nil {
			t.Errorf("splitPlatform(%q) expected error", bad)
		}
	}
}

func TestCrossBuildGo_RequiresBinaryAndVersion(t *testing.T) {
	if _, err := CrossBuildGo(context.Background(), CrossBuildConfig{Version: "1.0.0"}); err == nil {
		t.Error("expected error when BinaryName is empty")
	}
	if _, err := CrossBuildGo(context.Background(), CrossBuildConfig{BinaryName: "app"}); err == nil {
		t.Error("expected error when Version is empty")
	}
}

// TestSHA256File checks sha256File against the known digest of "hello",
// reproducible with `printf hello | sha256sum`.
func TestSHA256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got != want {
		t.Errorf("sha256 = %q, want %q", got, want)
	}
}

func TestChecksums_WritesSha256Manifest(t *testing.T) {
	dir := t.TempDir()
	setWorkDir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "app_linux"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Checksums(context.Background(), ChecksumConfig{}); err != nil {
		t.Fatalf("Checksums: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "dist", "checksums.txt"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	want := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  app_linux\n"
	if string(data) != want {
		t.Errorf("manifest = %q, want %q", string(data), want)
	}
}

func TestChecksums_NoFilesErrors(t *testing.T) {
	dir := t.TempDir()
	setWorkDir(t, dir)
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Checksums(context.Background(), ChecksumConfig{}); err == nil {
		t.Fatal("expected error when the dir has no files to checksum")
	}
}

// setWorkDir points sparkwing.WorkDir() at dir for the test, restoring
// the previous value on cleanup so the repo-relative helpers resolve
// against a scratch tree.
func setWorkDir(t *testing.T, dir string) {
	t.Helper()
	orig := sparkwing.WorkDir()
	sparkwing.SetWorkDir(dir)
	t.Cleanup(func() { sparkwing.SetWorkDir(orig) })
}

func TestListDirFiles_ExcludesManifestAndSorts(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.bin", "a.bin", "checksums.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := listDirFiles(dir, "checksums.txt")
	if err != nil {
		t.Fatalf("listDirFiles: %v", err)
	}
	want := []string{"a.bin", "b.bin"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("listDirFiles = %v, want %v", got, want)
	}
}
