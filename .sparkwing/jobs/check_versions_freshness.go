package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// CheckVersionsFreshness verifies every watched dependency in every go.mod
// under repoRoot is current: a direct require must be at or above the latest
// released tag, and a local replace target must not be behind origin/main.
// The error lists every problem, not just the first.
func CheckVersionsFreshness(ctx context.Context, repoRoot string) error {
	mods, err := findGoModFiles(repoRoot)
	if err != nil {
		return fmt.Errorf("scan go.mod files: %w", err)
	}
	var problems []string
	for _, modPath := range mods {
		bs, err := os.ReadFile(modPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", modPath, err)
		}
		f, err := modfile.Parse(modPath, bs, nil)
		if err != nil {
			return fmt.Errorf("parse %s: %w", modPath, err)
		}
		relMod, _ := filepath.Rel(repoRoot, modPath)
		for _, req := range f.Require {
			if !isWatchedModule(req.Mod.Path) {
				continue
			}
			if replace := findReplaceFor(f, req.Mod.Path); replace != nil {
				if !isLocalReplace(replace) {
					if msg := checkAgainstLatest(ctx, replace.New.Path, replace.New.Version, modPath); msg != "" {
						problems = append(problems, fmt.Sprintf("%s: %s", relMod, msg))
					}
					continue
				}
				localPath, err := resolveLocalReplacePath(replace.New.Path, modPath)
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: replace -> %s: %v", relMod, replace.New.Path, err))
					continue
				}
				behind, behindBy, err := localBehindRemote(ctx, localPath)
				if err != nil {
					problems = append(problems, fmt.Sprintf("%s: replace -> %s: %v", relMod, localPath, err))
					continue
				}
				if behind {
					problems = append(problems, fmt.Sprintf(
						"%s: %s replace -> %s is %d commits behind origin/main (pull or stop iterating)",
						relMod, req.Mod.Path, localPath, behindBy,
					))
				}
			} else {
				if msg := checkAgainstLatest(ctx, req.Mod.Path, req.Mod.Version, modPath); msg != "" {
					problems = append(problems, fmt.Sprintf("%s: %s", relMod, msg))
				}
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("version freshness:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

// watchedModulePrefixes scopes the check to the sparkwing ecosystem;
// third-party dependencies are skipped.
var watchedModulePrefixes = []string{
	"github.com/sparkwing-dev/sparkwing",
	"github.com/sparkwing-dev/sparks-core",
}

// maxAllowedMajor caps a module's semver major because the proxy carries
// v1.0.0+ tags pushed by mistake, and a proxy cache cannot be undone.
// Modules absent from the map have no cap.
var maxAllowedMajor = map[string]int{
	"github.com/sparkwing-dev/sparkwing": 0,
}

func isWatchedModule(path string) bool {
	for _, p := range watchedModulePrefixes {
		if path == p || strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func majorCapFor(modulePath string) int {
	if cap, ok := maxAllowedMajor[modulePath]; ok {
		return cap
	}
	return -1
}

func findGoModFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if name == "go.mod" {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func findReplaceFor(f *modfile.File, modulePath string) *modfile.Replace {
	for _, r := range f.Replace {
		if r.Old.Path == modulePath {
			return r
		}
	}
	return nil
}

func isLocalReplace(r *modfile.Replace) bool {
	p := r.New.Path
	return strings.HasPrefix(p, ".") || strings.HasPrefix(p, "/")
}

func resolveLocalReplacePath(target, modPath string) (string, error) {
	dir := filepath.Dir(modPath)
	abs, err := filepath.Abs(filepath.Join(dir, target))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("path does not exist: %w", err)
	}
	return abs, nil
}

// A checkout without origin/main may use another remote or default branch.
func localBehindRemote(ctx context.Context, localPath string) (bool, int, error) {
	root, err := gitCheckoutRoot(ctx, localPath)
	if err != nil {
		return false, 0, err
	}
	if root == "" {
		return false, 0, nil
	}
	remotes, err := captureGit(ctx, root, "remote")
	if err != nil {
		return false, 0, fmt.Errorf("list remotes: %w", err)
	}
	hasOrigin := false
	for _, remote := range strings.Fields(remotes) {
		if remote == "origin" {
			hasOrigin = true
		}
	}
	if !hasOrigin {
		return false, 0, nil
	}
	if fetchErr := runGit(ctx, root, "fetch", "--quiet", "origin", "refs/heads/main"); fetchErr != nil {
		if heads, err := captureGit(ctx, root, "ls-remote", "--heads", "origin", "refs/heads/main"); err == nil {
			found := false
			for _, line := range strings.Split(heads, "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[1] == "refs/heads/main" {
					found = true
				}
			}
			if !found {
				return false, 0, nil
			}
		}
		return false, 0, fmt.Errorf("fetch origin main: %w", fetchErr)
	}
	if err := runGit(ctx, root, "rev-parse", "--verify", "--quiet", "origin/main"); err != nil {
		return false, 0, fmt.Errorf("fetched origin main but origin/main does not resolve: %w", err)
	}
	out, err := captureGit(ctx, root, "rev-list", "--count", "HEAD..origin/main")
	if err != nil {
		return false, 0, fmt.Errorf("rev-list HEAD..origin/main: %w", err)
	}
	n := 0
	if s := strings.TrimSpace(out); s != "" {
		_, _ = fmt.Sscanf(s, "%d", &n)
	}
	return n > 0, n, nil
}

func gitCheckoutRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel")
	// safety: classify Git diagnostics consistently across locales.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 128 {
			message := strings.TrimSpace(string(exit.Stderr))
			if message == "fatal: this operation must be run in a work tree" {
				return "", nil
			}
			if strings.HasPrefix(message, "fatal: not a git repository (or any ") {
				metadata, statErr := hasGitMetadata(dir)
				if statErr != nil {
					return "", fmt.Errorf("inspect checkout metadata for %s: %w", dir, statErr)
				}
				if !metadata {
					return "", nil
				}
			}
		}
		return "", fmt.Errorf("locate checkout for %s: %w", dir, err)
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git returned an empty checkout root for %s", dir)
	}
	return root, nil
}

func hasGitMetadata(dir string) (bool, error) {
	path, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false, err
	}
	for {
		if _, err := os.Lstat(filepath.Join(path, ".git")); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false, nil
		}
		path = parent
	}
}

func checkAgainstLatest(ctx context.Context, modulePath, pinned, fromModFile string) string {
	if pinned == "" {
		return ""
	}
	cap := majorCapFor(modulePath)
	if cap >= 0 {
		if pinnedMajor, ok := semverMajor(pinned); ok && pinnedMajor > cap {
			return fmt.Sprintf(
				"%s pinned at %s but is capped at major v%d (the README states this module stays below v%d; v%d+ tags on the proxy were pushed by mistake)",
				modulePath, pinned, cap, cap+1, cap+1,
			)
		}
	}
	latest, err := latestReleasedVersion(ctx, modulePath, fromModFile)
	if err != nil {
		return fmt.Sprintf("%s: cannot resolve latest version (%v)", modulePath, err)
	}
	if semver.Compare(pinned, latest) >= 0 {
		return ""
	}
	return fmt.Sprintf("%s pinned at %s but %s is available (run `go get %s@%s`)",
		modulePath, pinned, latest, modulePath, latest)
}

func semverMajor(v string) (int, bool) {
	if !semver.IsValid(v) {
		return 0, false
	}
	maj := semver.Major(v)
	if !strings.HasPrefix(maj, "v") {
		return 0, false
	}
	n := 0
	if _, err := fmt.Sscanf(maj[1:], "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

// latestReleasedVersion runs from the consuming go.mod's directory so
// GOPROXY, GOPRIVATE, and replace directives are respected, and with
// GOWORK=off because a workspace local-replaces siblings and reports no
// tags for them.
func latestReleasedVersion(ctx context.Context, modulePath, fromModFile string) (string, error) {
	dir := filepath.Dir(fromModFile)
	out, err := captureCmdEnv(ctx, dir, []string{"GOWORK=off"}, "go", "list", "-m", "-versions", modulePath)
	if err != nil {
		return "", err
	}
	parts := strings.Fields(strings.TrimSpace(out))
	if len(parts) < 2 {
		return "", fmt.Errorf("no versions reported for %s", modulePath)
	}
	cap := majorCapFor(modulePath)
	var stable []string
	for _, v := range parts[1:] {
		if !semver.IsValid(v) || semver.Prerelease(v) != "" {
			continue
		}
		if cap >= 0 {
			if maj, ok := semverMajor(v); !ok || maj > cap {
				continue
			}
		}
		stable = append(stable, v)
	}
	if len(stable) == 0 {
		return "", fmt.Errorf("no stable releases for %s within cap", modulePath)
	}
	semver.Sort(stable)
	return stable[len(stable)-1], nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func captureGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func captureCmd(ctx context.Context, dir, name string, args ...string) (string, error) {
	return captureCmdEnv(ctx, dir, nil, name, args...)
}

func captureCmdEnv(ctx context.Context, dir string, extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.Output()
	return string(out), err
}
