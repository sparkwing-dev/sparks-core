package jobs

import (
	"context"
	"strings"
	"testing"
)

func TestMarkdownlintCommandIsPinned(t *testing.T) {
	const want = "npx --yes markdownlint-cli2@0.23.2"
	if markdownlintCommand != want {
		t.Fatalf("markdownlint command = %q, want exactly %q", markdownlintCommand, want)
	}
}

func TestGovulncheckModuleIsPinned(t *testing.T) {
	const want = "golang.org/x/vuln/cmd/govulncheck@v1.8.0"
	if govulncheckModule != want {
		t.Fatalf("govulncheck module = %q, want exactly %q", govulncheckModule, want)
	}
}

func TestLintAllModulesRefusesWithoutTheLinter(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := lintAllModules(context.Background())
	if err == nil {
		t.Fatal("the lint step ran with no golangci-lint on PATH")
	}
	if !strings.Contains(err.Error(), "golangci-lint is not on PATH") {
		t.Errorf("the refusal %q does not name the missing tool", err)
	}
	if strings.Contains(err.Error(), "module(s)") {
		t.Errorf("the missing linter reports a per-module failure instead of itself: %v", err)
	}
}
