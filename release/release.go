// Package release is sparks-core's shared release-and-publish toolkit.
// It backs the language-registry publish templates (github-release-go,
// npm-publish-package, pypi-publish-wheel) with the pieces those
// pipelines have in common, so the generated template bodies stay thin
// orchestration over one shared, tested gate.
//
// The surface splits into four concerns:
//
//   - Version derivation and gating: DeriveVersion resolves the release
//     version from an explicit parameter or a git tag, validates semver,
//     and can refuse a dirty working tree. GuardVersionFile reads the
//     version declared in a package.json / pyproject.toml and errors if
//     that version is already tagged, so a re-publish fails fast.
//   - Changelog extraction: ChangelogEntry pulls the notes for one
//     version (or the top released section) out of a Keep a Changelog
//     file, for use as release notes.
//   - Cross-platform Go builds: CrossBuildGo compiles a GOOS/GOARCH
//     matrix into a dist directory and returns the artifact paths;
//     Checksums writes a sha256sum-format manifest over them.
//   - Publishing: GitHubRelease, NpmPublish, and PyPIPublish wrap
//     `gh release create`, `npm publish`, and `twine upload` (or
//     `uv publish`) respectively.
//
// # Dry-run convention
//
// Every publishing operation reaches a remote registry, so each honors
// the SPARKWING_DRY_RUN convention: when the environment variable is
// non-empty (or the per-call DryRun field is set) the function logs the
// exact command argv it would run and returns success without executing
// it. This is what lets a freshly scaffolded release pipeline run green
// with no token configured. State-reading helpers (DeriveVersion,
// GuardVersionFile) and local build steps (CrossBuildGo, Checksums)
// always execute for real; they mutate nothing remote.
//
// The `gh`, `npm`, `twine`/`uv`, and `go` toolchains are expected on the
// runner PATH for the steps that shell out to them, matching the host-
// tool assumption the rest of sparks-core makes.
package release

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// dryRun reports whether SPARKWING_DRY_RUN is active. A non-empty value
// switches every publishing helper to echo-and-skip.
func dryRun() bool {
	return os.Getenv("SPARKWING_DRY_RUN") != ""
}

// echoArgv logs the exact command that would run under a dry run.
// Callers return nil after this so a mutating publish is a no-op that
// still shows its argv in the log stream.
func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

// semverPattern matches a Semantic Versioning string with an optional
// leading "v", an optional pre-release, and an optional build metadata
// suffix.
var semverPattern = regexp.MustCompile(
	`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?` +
		`(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
)

// IsSemver reports whether v is a valid Semantic Versioning string. A
// single optional leading "v" is accepted (e.g. both "1.2.3" and
// "v1.2.3").
func IsSemver(v string) bool {
	return semverPattern.MatchString(v)
}

// VersionConfig controls how DeriveVersion resolves a release version.
type VersionConfig struct {
	// Version, when non-empty, is used verbatim (still semver-validated
	// unless AllowNonSemver). It is the "explicit parameter" path and
	// takes precedence over any git tag.
	Version string
	// Describe, when true and Version is empty, resolves the version from
	// `git describe --tags --always`. This needs release tags in the
	// checkout (a full clone or `git fetch --tags`); with no matching tag
	// the `--always` fallback returns an abbreviated commit SHA instead of
	// failing, which then fails the semver check unless AllowNonSemver is
	// set. When false and Version is empty, DeriveVersion falls through to
	// DevFallback (or errors).
	Describe bool
	// Match is an optional `git describe --match` glob (e.g. "v*") used
	// only on the Describe path.
	Match string
	// RefuseDirty makes DeriveVersion error when the working tree has
	// uncommitted changes, so a release never captures un-committed work.
	RefuseDirty bool
	// DevFallback is returned when no Version and no tag resolve. Empty
	// means "error instead of falling back". Not semver-validated, so a
	// value like "0.0.0-dev" is fine.
	DevFallback string
	// AllowNonSemver skips semver validation of the resolved version, for
	// projects using a non-semver scheme (calendar versions, etc.).
	AllowNonSemver bool
}

// DeriveVersion resolves the release version from an explicit parameter
// or a git tag, validating that the result is semver unless
// AllowNonSemver is set. When RefuseDirty is set it first fails on a
// dirty working tree. This reads repository state only and always runs
// for real, including under SPARKWING_DRY_RUN.
func DeriveVersion(ctx context.Context, cfg VersionConfig) (string, error) {
	if cfg.RefuseDirty {
		dirty, err := workingTreeDirty(ctx)
		if err != nil {
			return "", err
		}
		if dirty {
			return "", fmt.Errorf("release: refusing to derive a version from a dirty working tree (commit or stash first)")
		}
	}

	version := strings.TrimSpace(cfg.Version)
	if version == "" && cfg.Describe {
		described, err := gitDescribe(ctx, cfg.Match)
		if err != nil {
			return "", err
		}
		version = described
	}
	if version == "" {
		if cfg.DevFallback != "" {
			return cfg.DevFallback, nil
		}
		return "", fmt.Errorf("release: no version resolved (set Version, enable Describe with a tagged commit, or set DevFallback)")
	}

	if !cfg.AllowNonSemver && !IsSemver(version) {
		return "", fmt.Errorf("release: %q is not a valid semantic version", version)
	}
	return version, nil
}

// gitDescribe runs `git describe --tags --always [--match <pat>]` and
// returns the trimmed output. With no matching tag the `--always`
// fallback yields an abbreviated commit SHA rather than failing.
func gitDescribe(ctx context.Context, match string) (string, error) {
	args := []string{"describe", "--tags", "--always"}
	if match != "" {
		args = append(args, "--match", match)
	}
	out, err := sparkwing.Exec(ctx, "git", args...).String()
	if err != nil {
		return "", fmt.Errorf("release: git describe: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// workingTreeDirty reports whether `git status --porcelain` shows any
// tracked or untracked change.
func workingTreeDirty(ctx context.Context) (bool, error) {
	out, err := sparkwing.Exec(ctx, "git", "status", "--porcelain").String()
	if err != nil {
		return false, fmt.Errorf("release: git status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}
