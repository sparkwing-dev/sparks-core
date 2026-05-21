package pipelines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStaticDeploy_HostBuild_PropagatesBuildExtraEnv is the ISS-033
// regression test: a host-mode build (BuildImage="") must inject
// BuildExtraEnv into the build subprocess so values like NEXT_EXPORT=1
// reach next.config.* via process.env.
func TestStaticDeploy_HostBuild_PropagatesBuildExtraEnv(t *testing.T) {
	work := t.TempDir()
	t.Setenv("SPARKWING_WORK_DIR", work)

	// BuildCmd writes the value of NEXT_EXPORT to a sentinel file. If
	// BuildExtraEnv wasn't propagated the file ends up empty, which
	// is the bug.
	sentinel := filepath.Join(work, "env-seen.txt")
	sd := &StaticDeploy{
		BuildCmd: `printf '%s' "$NEXT_EXPORT" > ` + sentinel,
		BuildExtraEnv: map[string]string{
			"NEXT_EXPORT": "1",
		},
	}

	ctx := context.Background()
	if err := sd.BuildOnly(ctx); err != nil {
		t.Fatalf("BuildOnly: %v", err)
	}

	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(got) != "1" {
		t.Fatalf("NEXT_EXPORT in subprocess = %q, want %q -- BuildExtraEnv was dropped", string(got), "1")
	}
}

// TestVerifyHTMLChunkRefs_FailsOnMissingChunk is the ISS-034
// regression test: when out/*.html references a chunk file that
// the build did not emit (the export-mode-not-engaged scenario),
// the chunk-ref check must surface a clear error so the deploy
// fails before S3 sync `--delete`s the live chunks.
func TestVerifyHTMLChunkRefs_FailsOnMissingChunk(t *testing.T) {
	out := t.TempDir()
	// Stale HTML from a prior export-mode build, pointing at a chunk
	// the current build did not emit.
	html := `<!doctype html><html><body>` +
		`<script src="/_next/static/chunks/main-OLDHASH.js"></script>` +
		`</body></html>`
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	// New build emitted a chunk with a different content hash; the
	// HTML reference doesn't match anything on disk.
	chunksDir := filepath.Join(out, "_next", "static", "chunks")
	if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chunksDir, "main-NEWHASH.js"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := verifyHTMLChunkRefs(out)
	if err == nil {
		t.Fatal("verifyHTMLChunkRefs: expected error on dangling chunk ref, got nil")
	}
	if !strings.Contains(err.Error(), "main-OLDHASH.js") {
		t.Fatalf("error should name the missing chunk, got: %v", err)
	}
}

// TestVerifyHTMLChunkRefs_PassesWhenChunksExist confirms the check
// is silent on a healthy build where every HTML reference resolves.
func TestVerifyHTMLChunkRefs_PassesWhenChunksExist(t *testing.T) {
	out := t.TempDir()
	chunksDir := filepath.Join(out, "_next", "static", "chunks")
	if err := os.MkdirAll(chunksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chunksDir, "main-abc.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cssDir := filepath.Join(out, "_next", "static", "css")
	if err := os.MkdirAll(cssDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cssDir, "site.css"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html><html>` +
		`<head><link rel="stylesheet" href="/_next/static/css/site.css"></head>` +
		`<body><script src="/_next/static/chunks/main-abc.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyHTMLChunkRefs(out); err != nil {
		t.Fatalf("verifyHTMLChunkRefs on healthy out: %v", err)
	}
}

// TestVerifyHTMLChunkRefs_NoOpWhenNoHTML guards against false
// positives on non-static deploys (e.g. a config-only sync).
func TestVerifyHTMLChunkRefs_NoOpWhenNoHTML(t *testing.T) {
	out := t.TempDir()
	if err := verifyHTMLChunkRefs(out); err != nil {
		t.Fatalf("verifyHTMLChunkRefs on empty dir: %v", err)
	}
}

// TestVerifyHTMLChunkRefs_ScansNestedRouteHTML covers the Next-style
// out/<route>/index.html layout.
func TestVerifyHTMLChunkRefs_ScansNestedRouteHTML(t *testing.T) {
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, "about"), 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<script src="/_next/static/chunks/missing.js"></script>`
	if err := os.WriteFile(filepath.Join(out, "about", "index.html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	err := verifyHTMLChunkRefs(out)
	if err == nil || !strings.Contains(err.Error(), "missing.js") {
		t.Fatalf("expected nested-route check to flag missing.js, got: %v", err)
	}
}

func TestExtractStaticRefs_DedupesAndScopes(t *testing.T) {
	html := `<script src="/_next/static/chunks/a.js"></script>` +
		`<script src="/_next/static/chunks/a.js"></script>` + // dup
		`<link href="/_next/static/css/x.css">` +
		`<a href="/about">route -- should be ignored</a>` +
		`<a href="https://cdn.example.com/foo.js">external -- ignored</a>`
	got := extractStaticRefs(html)
	want := []string{
		"/_next/static/chunks/a.js",
		"/_next/static/css/x.css",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
