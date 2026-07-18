// Package contentkey turns a set of tracked files into either a
// content-addressed cache key or a changed/unchanged decision, so a
// pipeline can replay unchanged work or skip a node whose inputs did
// not move.
//
// Two capabilities, one git-backed core:
//
//   - Cache keys. [OfPaths] and [Salted] fold the content hash of the
//     tracked files under a set of git pathspecs into a
//     [sparkwing.CacheKey]. Hand the returned function to a node's
//     .Cache modifier; an unchanged tree hashes to the same key and
//     replays the recorded result instead of re-running. See
//     `sparkwing docs read --topic caching` for the .Cache/sparkwing.Key model.
//
//   - Skip predicates. [Unchanged] and [Changed] compare the working
//     tree against a base ref with `git diff` and return a
//     [sparkwing.SkipPredicate]-shaped func. Hand [Unchanged] to a
//     node's .SkipIf modifier to soft-skip a job whose watched paths
//     match the base ref.
//
// Both read git state only (git ls-files, git hash-object, git diff);
// nothing here mutates the repository or any cloud resource, so the
// functions execute for real even under SPARKWING_DRY_RUN.
//
// File sets are resolved with `git ls-files`, so only tracked files are
// hashed and .gitignore is honored for free: ignored and untracked
// files never appear in a key or a diff. Globs are git pathspecs
// (matched by git, not the shell), and an empty glob list means every
// tracked file. The functions resolve paths against [sparkwing.WorkDir].
package contentkey

import (
	"context"
	"errors"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// keySchema versions the key layout. Bumping it invalidates every key
// this package has ever produced, independent of caller salt.
const keySchema = "contentkey/v1"

// OfPaths returns a cache-key function over the tracked files matching
// globs. Identical file content hashes to the same [sparkwing.CacheKey]
// across branches, rebases, and machines; any change to a matched file
// busts the key. With no globs it covers every tracked file.
//
//	job.Cache(contentkey.OfPaths("*.go", "go.mod", "go.sum"))
//
// The returned function reads git state and returns [sparkwing.NoCache]
// (running uncached) if the content cannot be hashed, e.g. outside a
// git repository.
func OfPaths(globs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return Salted("", globs...)
}

// Salted is [OfPaths] with a caller-supplied salt folded into the key.
// Bump the salt to invalidate every stored result at once when the
// content hash cannot see what changed, such as a toolchain or base
// image upgrade.
//
//	job.Cache(contentkey.Salted("v2", "*.go", "go.mod", "go.sum"))
func Salted(salt string, globs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return func(ctx context.Context) sparkwing.CacheKey {
		dir := workDir()
		key, err := contentKey(ctx, dir, salt, globs)
		if err != nil {
			sparkwing.Warn(ctx, "contentkey: hashing %v failed, running uncached: %v", globs, err)
			return sparkwing.NoCache
		}
		return key
	}
}

// Unchanged returns a skip predicate that reports true (skip) when no
// tracked file matching globs differs from baseRef. It fails safe: a
// missing baseRef or any git error returns false (run), so a broken
// base never silently skips work.
//
//	job.SkipIf(contentkey.Unchanged("origin/main", "src", "go.mod"))
func Unchanged(baseRef string, globs ...string) func(ctx context.Context) bool {
	return func(ctx context.Context) bool {
		dir := workDir()
		changed, known, err := changedVsBase(ctx, dir, baseRef, globs)
		if err != nil {
			sparkwing.Warn(ctx, "contentkey: diff against %q failed, not skipping: %v", baseRef, err)
			return false
		}
		if !known {
			return false
		}
		return !changed
	}
}

// Changed returns the inverse of [Unchanged]: true when a tracked file
// matching globs differs from baseRef. It shares Unchanged's fail-safe
// bias, so a missing baseRef or git error reports changed (run).
//
//	job.SkipIf(contentkey.Unchanged(base, paths...)) // skip when unchanged
//	deploy.RunIf(contentkey.Changed(base, paths...)) // act only when changed
func Changed(baseRef string, globs ...string) func(ctx context.Context) bool {
	unchanged := Unchanged(baseRef, globs...)
	return func(ctx context.Context) bool {
		return !unchanged(ctx)
	}
}

func workDir() string {
	if d := sparkwing.WorkDir(); d != "" {
		return d
	}
	return "."
}

// contentKey hashes the tracked files under globs in dir and folds the
// schema tag, salt, and per-file (path, blob-hash) pairs into a
// deterministic [sparkwing.CacheKey].
func contentKey(ctx context.Context, dir, salt string, globs []string) (sparkwing.CacheKey, error) {
	paths, err := trackedFiles(ctx, dir, globs)
	if err != nil {
		return "", err
	}
	parts := make([]any, 0, len(paths)+2)
	parts = append(parts, keySchema)
	if salt != "" {
		parts = append(parts, "salt="+salt)
	}
	if len(paths) > 0 {
		hashes, err := hashObjects(ctx, dir, paths)
		if err != nil {
			return "", err
		}
		if len(hashes) != len(paths) {
			return "", fmt.Errorf("git hash-object returned %d hashes for %d paths", len(hashes), len(paths))
		}
		for i, p := range paths {
			parts = append(parts, p+"="+hashes[i])
		}
	}
	return sparkwing.Key(parts...), nil
}

// trackedFiles lists the tracked files matching globs, in git's stable
// sorted order. An empty globs list lists every tracked file.
func trackedFiles(ctx context.Context, dir string, globs []string) ([]string, error) {
	args := append([]string{"ls-files", "--"}, globs...)
	return sparkwing.Exec(ctx, "git", args...).Dir(dir).Lines()
}

// hashObjects returns the git blob hash of each path's working-tree
// content, one per input path, in order. It does not write to the
// object store.
func hashObjects(ctx context.Context, dir string, paths []string) ([]string, error) {
	args := append([]string{"hash-object", "--"}, paths...)
	return sparkwing.Exec(ctx, "git", args...).Dir(dir).Lines()
}

// changedVsBase reports whether any tracked file matching globs differs
// between baseRef and the working tree. known is false when baseRef
// does not resolve, letting callers fail safe rather than treat a
// missing base as "unchanged".
func changedVsBase(ctx context.Context, dir, baseRef string, globs []string) (changed, known bool, err error) {
	if _, rerr := sparkwing.Exec(ctx, "git", "rev-parse", "--verify", "--quiet", baseRef+"^{commit}").Dir(dir).String(); rerr != nil {
		return false, false, nil
	}
	args := append([]string{"diff", "--quiet", baseRef, "--"}, globs...)
	_, derr := sparkwing.Exec(ctx, "git", args...).Dir(dir).Capture()
	if derr == nil {
		return false, true, nil
	}
	var exitErr *sparkwing.ExecError
	if errors.As(derr, &exitErr) && exitErr.ExitCode == 1 {
		return true, true, nil
	}
	return false, false, derr
}
