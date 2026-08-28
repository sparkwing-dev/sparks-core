// Package contentkey turns a set of tracked files into a
// content-addressed [sparkwing.CacheKey] for a node's .Memoize, or into a
// changed/unchanged predicate for its .SkipIf. Globs are git pathspecs
// resolved with `git ls-files` against [sparkwing.WorkDir], so only
// tracked files count and an empty glob list means all of them.
package contentkey

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// keySchema versions the key layout: bumping it invalidates every key this
// package has ever produced, independent of caller salt.
const keySchema = "contentkey/v1"

// OfPaths returns a cache-key function over the tracked files matching
// globs, for a node's .Memoize. It returns [sparkwing.NoCache] when the
// content cannot be hashed.
func OfPaths(globs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return Salted("", globs...)
}

// Salted is [OfPaths] with a caller-supplied salt folded into the key, to
// invalidate stored results when the content hash cannot see what changed.
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

// Unchanged returns a skip predicate that reports true when no tracked file
// matching globs differs from baseRef. It fails safe: a missing baseRef or
// any git error reports changed, so a broken base never skips work.
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

// Changed is the inverse of [Unchanged], sharing its fail-safe bias.
func Changed(baseRef string, globs ...string) func(ctx context.Context) bool {
	unchanged := Unchanged(baseRef, globs...)
	return func(ctx context.Context) bool {
		return !unchanged(ctx)
	}
}

// OfGoPackage is [OfPaths] over the same-module dependency closure of the
// Go package matching the `go list` pattern spec, plus extraGlobs. It
// returns [sparkwing.NoCache] when the closure cannot be resolved.
func OfGoPackage(spec string, extraGlobs ...string) func(ctx context.Context) sparkwing.CacheKey {
	return SaltedGoPackage("", spec, extraGlobs...)
}

// SaltedGoPackage is [OfGoPackage] with a caller salt folded in. spec is
// folded in too, so two packages sharing a salt never replay one another's
// result even if their file closures coincide.
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

// GoDeps returns the module-relative Go source, test, and embedded files in
// the same-module dependency closure of the package matching spec, as git
// pathspecs for [OfPaths]. It requires the `go` tool and a module in dir.
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

// mainModuleDir takes the base for relative paths from the same `go list`
// run as every package Dir, so filepath.Rel stays stable however the OS
// resolves symlinks in the checkout path.
func mainModuleDir(pkgs []goListPackage) string {
	for _, p := range pkgs {
		if p.Module != nil && p.Module.Main && p.Module.Dir != "" {
			return p.Module.Dir
		}
	}
	return ""
}

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

type goListModule struct {
	Main bool
	Dir  string
}

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

func contentKey(ctx context.Context, dir, salt string, globs []string) (sparkwing.CacheKey, error) {
	paths, err := trackedFiles(ctx, dir, globs)
	if err != nil {
		return "", err
	}
	paths, err = onDisk(dir, paths)
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

func trackedFiles(ctx context.Context, dir string, globs []string) ([]string, error) {
	args := append([]string{"ls-files", "--"}, globs...)
	return sparkwing.Exec(ctx, "git", args...).Dir(dir).Lines()
}

// onDisk drops tracked paths absent from the working tree, which `git
// ls-files` still lists before the deletion is staged. Only a confirmed
// absence drops a path: dropping on a transient Lstat fault would mint a
// different key for identical content and replay the wrong result.
func onDisk(dir string, paths []string) ([]string, error) {
	kept := paths[:0:0]
	for _, p := range paths {
		_, err := os.Lstat(filepath.Join(dir, p))
		switch {
		case err == nil:
			kept = append(kept, p)
		case errors.Is(err, fs.ErrNotExist):
			// safety: deleted but still tracked, so absence folds into the key
		default:
			return nil, fmt.Errorf("stat tracked file: %w", err)
		}
	}
	return kept, nil
}

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

// changedVsBase reports known=false when baseRef does not resolve, so a
// missing base is never treated as "unchanged".
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
