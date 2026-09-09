package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sw "github.com/sparkwing-dev/sparkwing/sparkwing"
)

func TestLintScopesCacheToWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	t.Setenv("GOLANGCI_LINT_CACHE", filepath.Join(root, "shared-cache"))
	stubDir := t.TempDir()
	seen := filepath.Join(root, "seen")
	t.Setenv("LINT_CACHE_SEEN", seen)
	stub := "#!/bin/sh\nprintf '%s' \"$GOLANGCI_LINT_CACHE\" > \"$LINT_CACHE_SEEN\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, "golangci-lint"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stubDir, "go"), []byte("#!/bin/sh\necho example.com/fixture\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := sw.WorkDir()
	t.Cleanup(func() { sw.SetWorkDir(prev) })
	var first string
	for _, parent := range []string{"a", "b", "a"} {
		dir := filepath.Join(root, parent, "same-branch")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sw.SetWorkDir(dir)
		if err := lintAllModules(context.Background()); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(seen)
		if err != nil {
			t.Fatal(err)
		}
		want := sw.ToolCacheDir("golangci-lint")
		if string(got) != want {
			t.Fatalf("cache = %q, want %q", got, want)
		}
		if first == "" {
			first = string(got)
		} else if parent == "b" && first == string(got) {
			t.Fatal("sibling worktrees share cache")
		} else if parent == "a" && first != string(got) {
			t.Fatal("worktree cache changed across runs")
		}
	}
}
