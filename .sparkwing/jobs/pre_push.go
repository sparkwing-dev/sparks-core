package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// PrePush gates pushes to main. Each check runs as its own parallel
// Work step so failures surface independently in the dashboard.
//
// sparks-core is a multi-module monorepo: lint, test, vuln, and
// tidy run against every go.mod discovered under the repo root
// (excluding vendor / node_modules / .git). The non-module checks
// (replace ban, go.work ban, version freshness, shellcheck,
// markdownlint) run once at the repo level.
//
// Wire it to git: declare `pre_push:` in pipelines.yaml and run
// `sparkwing pipeline hooks install`. Tooling assumed on PATH:
// golangci-lint, shellcheck, markdownlint-cli2.
type PrePush struct{ sparkwing.Base }

func (PrePush) ShortHelp() string {
	return "Pre-push gate: lint, test -race, vuln, freshness, no replace, no go.work"
}

func (PrePush) Help() string {
	return "Final gate before main. Each check runs as its own Work step. " +
		"Per-module checks (golangci-lint, `go test -race`, govulncheck, " +
		"`go mod tidy` drift) iterate every go.mod under the repo. Repo-" +
		"level checks: no `replace` lines, no committed go.work / go.work.sum, " +
		"sparkwing-ecosystem version-freshness, shellcheck, markdownlint."
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

// Work declares one step per check so they dispatch in parallel and
// surface independently in the dashboard. No Needs() edges -- the
// checks are mutually independent.
func (p *PrePush) Work(w *sparkwing.Work) (*sparkwing.WorkStep, error) {
	sparkwing.Step(w, "no-replace", checkNoReplaceDirectivesInCommittedGoMods)
	sparkwing.Step(w, "no-go-work", checkNoCommittedGoWorkFiles)
	sparkwing.Step(w, "no-raw-kubectl", checkNoRawKubectl)
	sparkwing.Step(w, "no-raw-aws", checkNoRawAWS)
	sparkwing.Step(w, "module-layering", checkModuleLayering)
	sparkwing.Step(w, "tidy", tidyAllModules)
	sparkwing.Step(w, "version-freshness", checkVersionFreshness)
	sparkwing.Step(w, "golangci-lint", lintAllModules)
	sparkwing.Step(w, "test-race", testRaceAllModules)
	sparkwing.Step(w, "govulncheck", govulncheckAllModules)
	sparkwing.Step(w, "shellcheck", runShellcheck)
	sparkwing.Step(w, "markdownlint", runMarkdownlint)
	return nil, nil
}

// allModuleDirs returns the directories containing each tracked
// go.mod under the repo, sorted for deterministic output.
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

// tidyAllModules runs `go mod tidy` in every module and fails if
// any produced a diff against HEAD. tidy itself can fail in
// workspaces with unreleased local siblings; swallow it and rely
// on the captured diff as the signal.
//
// Capture-output check rather than `git diff --quiet`: the latter's
// exit code has been observed to occasionally report dirty under
// sparkwing.Bash even when the tree is clean. Combining tidy + diff
// into one bash invocation also avoids any chance of the diff
// observing pre-tidy state.
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

// govulncheckAllModules compiles govulncheck against the current
// toolchain so the scan reports against the actual stdlib version
// the project builds with. A standalone `govulncheck` on PATH is
// frozen to the Go version that installed it and produces stale
// false-positives after a system Go upgrade.
func govulncheckAllModules(ctx context.Context) error {
	return forEachModuleDir(ctx, "govulncheck", "go run golang.org/x/vuln/cmd/govulncheck@latest ./...")
}

// forEachModuleDir runs cmd in each module directory and aggregates
// failures so the caller sees every offending module in one report
// instead of just the first. Modules with no Go packages (e.g. a
// monorepo root that only carries the go.work) are skipped silently.
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

// moduleHasNoPackages reports whether `go list ./...` from dir
// matches zero packages. Empty modules legitimately exist in
// monorepo roots (a parent go.mod that only carries module metadata)
// and should not fail per-module checks.
func moduleHasNoPackages(ctx context.Context, dir string) (bool, error) {
	out, err := sparkwing.Bash(ctx, fmt.Sprintf(`cd %q && go list ./... 2>&1 || true`, dir)).String()
	if err != nil {
		return false, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return true, nil
	}
	// `go list` prints "matched no packages" to stderr; with 2>&1 it
	// lands in stdout. Treat that as the empty signal.
	if strings.Contains(out, "matched no packages") {
		return true, nil
	}
	return false, nil
}

func checkVersionFreshness(ctx context.Context) error {
	return CheckVersionsFreshness(ctx, sparkwing.WorkDir())
}

func runShellcheck(ctx context.Context) error {
	_, err := sparkwing.Bash(ctx, "bash bin/check-shell.sh").Run()
	return err
}

func runMarkdownlint(ctx context.Context) error {
	_, err := sparkwing.Bash(ctx, "markdownlint-cli2").Run()
	return err
}

// checkNoReplaceDirectivesInCommittedGoMods refuses to let any
// committed go.mod ship with a `replace` line. Replace directives
// are intended for local iteration; once they leak into main they
// break every consumer of this repo (Go module proxy can't resolve
// a local-path replace, so anyone cloning will fail to build).
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

// checkNoCommittedGoWorkFiles refuses to let a workspace file ship.
// `go.work` and `go.work.sum` are local-iteration scaffolding (they
// point at relative paths on the developer's machine) and break
// builds for anyone who clones the repo.
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
