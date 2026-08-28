// Package release derives and gates release versions, extracts changelog
// entries, cross-builds Go binaries with checksums, and publishes to GitHub,
// npm, and PyPI. Publishing honors SPARKWING_DRY_RUN (or a call's DryRun
// field) by echoing argv, so a scaffolded pipeline runs green with no token;
// version reads and local builds always execute.
package release

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func dryRun() bool {
	return os.Getenv("SPARKWING_DRY_RUN") != ""
}

func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

var semverPattern = regexp.MustCompile(
	`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-((?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?` +
		`(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`,
)

// IsSemver reports whether v is valid semver, with an optional leading "v".
func IsSemver(v string) bool {
	return semverPattern.MatchString(v)
}

// VersionConfig controls how DeriveVersion resolves a release version.
type VersionConfig struct {
	// Version is used verbatim and takes precedence over any git tag.
	Version string
	// Describe resolves the version from `git describe --tags --always`,
	// which needs release tags in the checkout. With no matching tag the
	// --always fallback yields a commit SHA, failing the semver check.
	Describe bool
	// Match is a `git describe --match` glob, used only on the Describe path.
	Match string
	// RefuseDirty errors on uncommitted changes, so a release never captures
	// un-committed work.
	RefuseDirty bool
	// DevFallback is returned when nothing else resolves; empty errors
	// instead. It is not semver-validated.
	DevFallback string
	// AllowNonSemver skips semver validation, for calendar and similar schemes.
	AllowNonSemver bool
}

// DeriveVersion resolves the release version from cfg.Version or a git tag,
// validating semver unless AllowNonSemver is set.
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

func workingTreeDirty(ctx context.Context) (bool, error) {
	out, err := sparkwing.Exec(ctx, "git", "status", "--porcelain").String()
	if err != nil {
		return false, fmt.Errorf("release: git status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}
