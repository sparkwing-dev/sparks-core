package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// PrePush gates pushes to main. Per-module checks run against every go.mod
// under the repo root; the rest run once at the repo level. Wire it to git
// by declaring `pre_push:` in pipelines.yaml and running `sparkwing pipeline
// hooks install`. It assumes golangci-lint, shellcheck, and
// markdownlint-cli2 on PATH.
type PrePush struct{ sparkwing.Base }

func (PrePush) ShortHelp() string {
	return "Pre-push gate: lint, test -race, vuln, freshness, no replace, no go.work"
}

func (PrePush) Help() string {
	return "Final gate before main. Each check runs as its own Work step. " +
		"Per-module checks (golangci-lint, `go test -race`, govulncheck, " +
		"`go mod tidy` drift) iterate every go.mod under the repo. Repo-" +
		"level checks: no `replace` lines, no committed go.work / go.work.sum, " +
		"shellcheck, markdownlint. SDK staleness is reported by the " +
		"sparks-core-sdk-staleness chore rather than gated here, because no " +
		"push can clear it and a gate that fails on every push teaches people " +
		"to ignore it."
}

func (PrePush) Examples() []sparkwing.Example {
	return []sparkwing.Example{
		{Comment: "Manually invoke the pre-push gate", Command: "sparkwing run pre-push"},
	}
}

func (p *PrePush) Plan(_ context.Context, plan *sparkwing.Plan, _ sparkwing.NoInputs, rc sparkwing.RunContext) error {
	sparkwing.Job(plan, rc.Pipeline, p)
	return nil
}

// Work declares one step per check, with no Needs() edges, so they dispatch
// in parallel and surface independently.
func (p *PrePush) Work(w *sparkwing.Work) (*sparkwing.WorkStep, error) {
	sparkwing.Step(w, "no-replace", checkNoReplaceDirectivesInCommittedGoMods)
	sparkwing.Step(w, "no-go-work", checkNoCommittedGoWorkFiles)
	sparkwing.Step(w, "no-raw-kubectl", checkNoRawKubectl)
	sparkwing.Step(w, "no-raw-aws", checkNoRawAWS)
	sparkwing.Step(w, "module-layering", checkModuleLayering)
	sparkwing.Step(w, "tidy", tidyAllModules)
	sparkwing.Step(w, "golangci-lint", lintAllModules)
	sparkwing.Step(w, "test-race", testRaceAllModules)
	sparkwing.Step(w, "govulncheck", govulncheckAllModules)
	sparkwing.Step(w, "shellcheck", runShellcheck)
	sparkwing.Step(w, "markdownlint", runMarkdownlint)
	return nil, nil
}

func allModuleDirs() ([]string, error) {
	mods, err := findGoModFiles(sparkwing.WorkDir())
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(mods))
	for _, m := range mods {
		dirs = append(dirs, filepath.Dir(m))
	}
	return dirs, nil
}

// tidyAllModules swallows tidy's own error, which fires in workspaces with
// unreleased local siblings, and relies on the captured diff instead. It
// captures output rather than using `git diff --quiet`, whose exit code has
// been seen reporting dirty on a clean tree under sparkwing.Bash, and runs
// both in one bash invocation so the diff cannot observe pre-tidy state.
func tidyAllModules(ctx context.Context) error {
	dirs, err := allModuleDirs()
	if err != nil {
		return err
	}
	var dirty []string
	for _, dir := range dirs {
		rel, _ := filepath.Rel(sparkwing.WorkDir(), dir)
		if rel == "" {
			rel = "."
		}
		cmd := fmt.Sprintf(
			`go -C %q mod tidy 2>/dev/null || true; git diff --no-color -- %q %q`,
			rel, filepath.Join(rel, "go.mod"), filepath.Join(rel, "go.sum"),
		)
		out, _ := sparkwing.Bash(ctx, cmd).String()
		if strings.TrimSpace(out) != "" {
			dirty = append(dirty, rel)
		}
	}
	if len(dirty) > 0 {
		return fmt.Errorf("`go mod tidy` produced a diff in %d module(s); run it locally and commit:\n    %s",
			len(dirty), strings.Join(dirty, "\n    "))
	}
	return nil
}

func lintAllModules(ctx context.Context) error {
	return forEachModuleDir(ctx, "golangci-lint", "golangci-lint run ./...")
}

func testRaceAllModules(ctx context.Context) error {
	return forEachModuleDir(ctx, "go test -race", "go test -race ./...")
}

// govulncheckAllModules compiles govulncheck against the current toolchain,
// because a standalone binary on PATH is frozen to the Go version that
// installed it and false-positives after a system Go upgrade.
func govulncheckAllModules(ctx context.Context) error {
	return forEachModuleDir(ctx, "govulncheck", "go run golang.org/x/vuln/cmd/govulncheck@latest ./...")
}

// forEachModuleDir aggregates failures so every offending module shows in
// one report, and skips modules with no Go packages.
func forEachModuleDir(ctx context.Context, label, cmd string) error {
	dirs, err := allModuleDirs()
	if err != nil {
		return err
	}
	var failures []string
	for _, dir := range dirs {
		rel, _ := filepath.Rel(sparkwing.WorkDir(), dir)
		if rel == "" {
			rel = "."
		}
		if empty, err := moduleHasNoPackages(ctx, rel); err == nil && empty {
			continue
		}
		if _, err := sparkwing.Bash(ctx, fmt.Sprintf(`cd %q && %s`, rel, cmd)).Run(); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", rel, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s failed in %d module(s):\n  - %s",
			label, len(failures), strings.Join(failures, "\n  - "))
	}
	return nil
}

// moduleHasNoPackages exists because a monorepo root go.mod carrying only
// module metadata should not fail per-module checks.
func moduleHasNoPackages(ctx context.Context, dir string) (bool, error) {
	out, err := sparkwing.Bash(ctx, fmt.Sprintf(`cd %q && go list ./... 2>&1 || true`, dir)).String()
	if err != nil {
		return false, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return true, nil
	}
	if strings.Contains(out, "matched no packages") {
		return true, nil
	}
	return false, nil
}

func runShellcheck(ctx context.Context) error {
	_, err := sparkwing.Bash(ctx, "bash bin/check-shell.sh").Run()
	return err
}

func runMarkdownlint(ctx context.Context) error {
	_, err := sparkwing.Bash(ctx, "markdownlint-cli2").Run()
	return err
}

// checkNoReplaceDirectivesInCommittedGoMods refuses a committed `replace`
// line, which the module proxy cannot resolve, so anyone cloning fails to
// build.
func checkNoReplaceDirectivesInCommittedGoMods(ctx context.Context) error {
	out, err := sparkwing.Bash(ctx,
		`git ls-files '*go.mod' | xargs -I {} grep -lE '^replace ' {} 2>/dev/null || true`,
	).String()
	if err != nil {
		return fmt.Errorf("scan go.mod files: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	files := strings.Split(out, "\n")
	return fmt.Errorf(
		"refusing to push: %d committed go.mod file(s) contain `replace` lines (remove the replace and pin a released tag):\n    %s",
		len(files), strings.Join(files, "\n    "),
	)
}

// checkNoCommittedGoWorkFiles refuses a committed workspace file, which
// points at relative paths on one developer's machine.
func checkNoCommittedGoWorkFiles(ctx context.Context) error {
	out, err := sparkwing.Bash(ctx,
		`git ls-files | grep -E '(^|/)go\.work(\.sum)?$' || true`,
	).String()
	if err != nil {
		return fmt.Errorf("scan go.work files: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	files := strings.Split(out, "\n")
	return fmt.Errorf(
		"refusing to push: %d committed go.work file(s) (remove + add to .gitignore):\n    %s",
		len(files), strings.Join(files, "\n    "),
	)
}

func init() {
	sparkwing.Register("pre-push", func() sparkwing.Pipeline[sparkwing.NoInputs] { return &PrePush{} })
}
