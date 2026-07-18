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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
//
// The comparison uses `git diff`, which consults the index stat cache.
// On a reused workspace where a checkout or restore touched file mtimes
// without changing content, git may report a difference and the
// predicate reports changed (run). The bias is fail-safe: it never skips
// work that should run, only occasionally reruns work that is identical.
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

// OfGoPackage returns a cache-key function over the same-module
// dependency closure of the Go package matching spec, plus any extra git
// pathspecs (a repo-wide go.mod / go.sum, shared testdata). Editing the
// package, or any package in the same module it transitively imports,
// busts the key; editing an unrelated package does not. It is [OfPaths]
// scoped to one package's real dependency footprint via `go list`, the
// building block for per-package test caching in a monorepo.
//
//	job.Cache(contentkey.OfGoPackage("./integration", "go.mod", "go.sum"))
//
// spec is a `go list` package pattern that resolves to a single package
// (`.`, `./integration`, or a full import path). The returned function
// reads git and go state and returns [sparkwing.NoCache] (running
// uncached) when the closure cannot be resolved, e.g. outside a Go
// module or when the `go` tool is unavailable.
func OfGoPackage(spec string, extraGlobs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return SaltedGoPackage("", spec, extraGlobs...)
}

// SaltedGoPackage is [OfGoPackage] with a caller-supplied salt folded
// into the key, like [Salted]. spec is always folded in as well, so two
// packages sharing a salt never replay one another's stored result even
// if their file closures happen to coincide.
//
//	job.Cache(contentkey.SaltedGoPackage("v2", "./integration", "go.mod"))
func SaltedGoPackage(salt, spec string, extraGlobs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return func(ctx context.Context) sparkwing.CacheKey {
		dir := workDir()
		files, err := GoDeps(ctx, dir, spec)
		if err != nil {
			sparkwing.Warn(ctx, "contentkey: resolving go deps of %q failed, running uncached: %v", spec, err)
			return sparkwing.NoCache
		}
		paths := make([]string, 0, len(files)+len(extraGlobs))
		paths = append(paths, files...)
		paths = append(paths, extraGlobs...)
		key, err := contentKey(ctx, dir, salt+"\x00gopkg="+spec, paths)
		if err != nil {
			sparkwing.Warn(ctx, "contentkey: hashing go deps of %q failed, running uncached: %v", spec, err)
			return sparkwing.NoCache
		}
		return key
	}
}

// GoDeps returns the repo-relative Go source files in the same-module
// dependency closure of the package matching spec: the package's own
// source, test, and embedded files, plus the non-test source of every
// same-module package it transitively imports (test-only imports
// included). Standard-library packages and files outside the main module
// are excluded. Paths are repo-relative git pathspecs suitable for
// [OfPaths] / [Salted]; they are resolved with `go list`, so an edit to
// the package or a dependency changes the set while an unrelated edit
// does not.
//
// spec is a `go list` package pattern resolving to a single package.
// GoDeps requires the `go` tool and a module rooted at dir; a `go list`
// failure (no module, unresolved import) is returned as an error.
func GoDeps(ctx context.Context, dir, spec string) ([]string, error) {
	pkgs, err := goListDeps(ctx, dir, spec)
	if err != nil {
		return nil, err
	}
	root := mainModuleDir(pkgs)
	if root == "" {
		return nil, nil
	}
	set := map[string]struct{}{}
	for _, p := range pkgs {
		if p.Standard || p.Dir == "" || p.Module == nil || !p.Module.Main {
			continue
		}
		files := make([]string, 0, len(p.GoFiles)+len(p.CgoFiles)+len(p.EmbedFiles))
		files = append(files, p.GoFiles...)
		files = append(files, p.CgoFiles...)
		files = append(files, p.EmbedFiles...)
		if !p.DepOnly {
			files = append(files, p.TestGoFiles...)
			files = append(files, p.XTestGoFiles...)
			files = append(files, p.TestEmbedFiles...)
			files = append(files, p.XTestEmbedFiles...)
		}
		for _, f := range files {
			if filepath.IsAbs(f) {
				continue
			}
			rel, err := filepath.Rel(root, filepath.Join(p.Dir, f))
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			set[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// mainModuleDir returns the directory of the module being built, taken
// from any package `go list` marks as belonging to the main module. It
// is the base for repo-relative paths: because it and every package Dir
// come from the same `go list` run, filepath.Rel between them is stable
// regardless of how the OS resolves symlinks in the checkout path.
func mainModuleDir(pkgs []goListPackage) string {
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main && p.Module.Dir != "" {
			return p.Module.Dir
		}
	}
	return ""
}

// goListPackage is the subset of `go list -json` fields GoDeps reads.
type goListPackage struct {
	Dir             string
	Standard        bool
	DepOnly         bool
	Module          *goListModule
	GoFiles         []string
	CgoFiles        []string
	EmbedFiles      []string
	TestGoFiles     []string
	XTestGoFiles    []string
	TestEmbedFiles  []string
	XTestEmbedFiles []string
}

// goListModule is the subset of a `go list -json` Module object GoDeps
// reads: Main marks the package as belonging to the module being built
// (how the closure is scoped to intra-repo dependencies), and Dir is the
// module root the returned paths are made relative to.
type goListModule struct {
	Main bool
	Dir  string
}

// goListDeps runs `go list -deps -test -json spec` in dir and decodes the
// concatenated JSON object stream it prints (one object per package).
func goListDeps(ctx context.Context, dir, spec string) ([]goListPackage, error) {
	res, err := sparkwing.Exec(ctx, "go", "list", "-deps", "-test", "-json", spec).Dir(dir).Capture()
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(res.Stdout))
	var pkgs []goListPackage
	for {
		var p goListPackage
		if err := dec.Decode(&p); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
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
	paths = onDisk(dir, paths)
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

// onDisk drops tracked paths that are absent from the working tree.
// `git ls-files` still lists a tracked file after it is deleted but not
// yet staged; hashing such a path errors and would bust the whole key.
// Dropping it instead lets the deletion register as an ordinary key
// change (the path's (path, hash) pair disappears) rather than forcing
// an uncached run.
func onDisk(dir string, paths []string) []string {
	kept := paths[:0:0]
	for _, p := range paths {
		if _, err := os.Lstat(filepath.Join(dir, p)); err == nil {
			kept = append(kept, p)
		}
	}
	return kept
}

// hashObjects returns the git blob hash of each path's working-tree
// content, one per input path, in order. It does not write to the
// object store. Paths are hashed in argv-size-bounded batches so an
// all-tracked-files invocation on a large monorepo does not exceed the
// OS argument-length limit.
func hashObjects(ctx context.Context, dir string, paths []string) ([]string, error) {
	// hack: batch argv under ARG_MAX; sparkwing.Cmd has no stdin for --stdin-paths.
	const maxArgvBytes = 100_000
	hashes := make([]string, 0, len(paths))
	for start := 0; start < len(paths); {
		end, budget := start, 0
		for end < len(paths) {
			cost := len(paths[end]) + 1
			if end > start && budget+cost > maxArgvBytes {
				break
			}
			budget += cost
			end++
		}
		args := append([]string{"hash-object", "--"}, paths[start:end]...)
		batch, err := sparkwing.Exec(ctx, "git", args...).Dir(dir).Lines()
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, batch...)
		start = end
	}
	return hashes, nil
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
