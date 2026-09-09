// Package contentkey provides cache keys and skip predicates over tracked files.
// Globs are Git pathspecs relative to [sparkwing.WorkDir]; an empty list selects all tracked files.
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

// keySchema changes invalidate stored results independently of caller salt.
const keySchema = "contentkey/v1"

// OfPaths returns a cache-key function over the tracked files matching
// globs. [sparkwing.WorkDir] must identify the project. Resolution errors fail
// the node before cache lookup or execution.
func OfPaths(globs ...string) sparkwing.CacheKeyFn {
	return Salted("", globs...)
}

// Salted is [OfPaths] with a caller-supplied salt folded into the key, to
// invalidate stored results when the content hash cannot see what changed.
func Salted(salt string, globs ...string) sparkwing.CacheKeyFn {
	return func(runContext context.Context) (sparkwing.CacheKey, error) {
		if err := runContext.Err(); err != nil {
			return "", err
		}
		directory := sparkwing.WorkDir()
		if directory == "" {
			return "", errors.New("cache resolution requires an SDK working directory")
		}
		key, err := contentKey(runContext, directory, salt, globs)
		if err != nil {
			return "", fmt.Errorf("hash paths %v: %w", globs, err)
		}
		return key, nil
	}
}

// Unchanged reports whether matching tracked files equal baseRef.
// Missing references and Git failures return false.
func Unchanged(baseRef string, globs ...string) func(runContext context.Context) bool {
	return func(runContext context.Context) bool {
		directory := workDir()
		changed, known, err := changedVsBase(runContext, directory, baseRef, globs)
		if err != nil {
			sparkwing.Warn(runContext, "contentkey: diff against %q failed, not skipping: %v", baseRef, err)
			return false
		}
		if !known {
			return false
		}
		return !changed
	}
}

// Changed is the inverse of [Unchanged].
func Changed(baseRef string, globs ...string) func(runContext context.Context) bool {
	unchanged := Unchanged(baseRef, globs...)
	return func(runContext context.Context) bool {
		return !unchanged(runContext)
	}
}

// OfGoPackage is [OfPaths] over the same-module dependency closure of the
// Go package matching spec, plus extraGlobs. Dependency and hashing failures
// propagate through the returned resolver.
func OfGoPackage(spec string, extraGlobs ...string) sparkwing.CacheKeyFn {
	return SaltedGoPackage("", spec, extraGlobs...)
}

// SaltedGoPackage is [OfGoPackage] with a caller salt folded in. spec is
// folded in too, so two packages sharing a salt never replay one another's
// result even if their file closures coincide.
func SaltedGoPackage(salt, spec string, extraGlobs ...string) sparkwing.CacheKeyFn {
	return func(runContext context.Context) (sparkwing.CacheKey, error) {
		if err := runContext.Err(); err != nil {
			return "", err
		}
		directory := sparkwing.WorkDir()
		if directory == "" {
			return "", errors.New("cache resolution requires an SDK working directory")
		}
		files, err := GoDeps(runContext, directory, spec)
		if err != nil {
			return "", fmt.Errorf("resolve Go dependencies for %q: %w", spec, err)
		}
		paths := make([]string, 0, len(files)+len(extraGlobs))
		paths = append(paths, files...)
		paths = append(paths, extraGlobs...)
		key, err := contentKey(runContext, directory, salt+"\x00gopkg="+spec, paths)
		if err != nil {
			return "", fmt.Errorf("hash Go dependencies for %q: %w", spec, err)
		}
		return key, nil
	}
}

// GoDeps returns the module-relative Go source, test, and embedded files in
// the same-module dependency closure of the package matching spec, as git
// pathspecs for [OfPaths]. It requires the `go` tool and a module in directory.
func GoDeps(runContext context.Context, directory, spec string) ([]string, error) {
	packages, err := goListDeps(runContext, directory, spec)
	if err != nil {
		return nil, err
	}
	root, err := mainModuleDir(packages)
	if err != nil {
		return nil, fmt.Errorf("resolve main module for %q: %w", spec, err)
	}
	set := map[string]struct{}{}
	for _, listedPackage := range packages {
		if listedPackage.Standard || listedPackage.Module == nil || !listedPackage.Module.Main || listedPackage.Module.Dir != root {
			continue
		}
		if listedPackage.Dir == "" {
			return nil, errors.New("main-module package has no source directory")
		}
		files := make([]string, 0, len(listedPackage.GoFiles)+len(listedPackage.CgoFiles)+len(listedPackage.EmbedFiles))
		files = append(files, listedPackage.GoFiles...)
		files = append(files, listedPackage.CgoFiles...)
		files = append(files, listedPackage.EmbedFiles...)
		if !listedPackage.DepOnly {
			files = append(files, listedPackage.TestGoFiles...)
			files = append(files, listedPackage.XTestGoFiles...)
			files = append(files, listedPackage.TestEmbedFiles...)
			files = append(files, listedPackage.XTestEmbedFiles...)
		}
		for _, file := range files {
			if filepath.IsAbs(file) {
				// SAFETY: Go's synthetic test driver names generated files in its build cache.
				continue
			}
			relativePath, err := moduleRelativePath(root, listedPackage.Dir, file)
			if err != nil {
				return nil, err
			}
			set[filepath.ToSlash(relativePath)] = struct{}{}
		}
	}
	paths := make([]string, 0, len(set))
	for file := range set {
		paths = append(paths, file)
	}
	sort.Strings(paths)
	return paths, nil
}

// mainModuleDir uses the package listing's root so symlink resolution agrees with package paths.
func mainModuleDir(packages []goListPackage) (string, error) {
	root := ""
	for _, listedPackage := range packages {
		if listedPackage.DepOnly || listedPackage.Module == nil || !listedPackage.Module.Main {
			continue
		}
		if listedPackage.Module.Dir == "" {
			return "", errors.New("target package has no main module directory")
		}
		if root != "" && root != listedPackage.Module.Dir {
			return "", errors.New("target packages belong to different main modules")
		}
		root = listedPackage.Module.Dir
	}
	if root == "" {
		return "", errors.New("target package has no main module")
	}
	return root, nil
}

func moduleRelativePath(root, packageDirectory, file string) (string, error) {
	path := filepath.Join(packageDirectory, file)
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("resolve source path %q relative to main module %q: %w", path, root, err)
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source path %q is outside main module %q", path, root)
	}
	return relativePath, nil
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

func goListDeps(runContext context.Context, directory, spec string) ([]goListPackage, error) {
	result, err := sparkwing.Exec(runContext, "go", "list", "-deps", "-test", "-json", spec).Dir(directory).Capture()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(result.Stdout))
	var packages []goListPackage
	for {
		var listedPackage goListPackage
		if err := decoder.Decode(&listedPackage); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode go list output: %w", err)
		}
		packages = append(packages, listedPackage)
	}
	return packages, nil
}

func workDir() string {
	if directory := sparkwing.WorkDir(); directory != "" {
		return directory
	}
	return "."
}

func contentKey(runContext context.Context, directory, salt string, globs []string) (sparkwing.CacheKey, error) {
	paths, err := trackedFiles(runContext, directory, globs)
	if err != nil {
		return "", err
	}
	paths, err = onDisk(directory, paths)
	if err != nil {
		return "", err
	}
	parts := make([]any, 0, len(paths)+2)
	parts = append(parts, keySchema)
	if salt != "" {
		parts = append(parts, "salt="+salt)
	}
	if len(paths) > 0 {
		hashes, err := hashObjects(runContext, directory, paths)
		if err != nil {
			return "", err
		}
		if len(hashes) != len(paths) {
			return "", fmt.Errorf("git hash-object returned %d hashes for %d paths", len(hashes), len(paths))
		}
		for i, path := range paths {
			parts = append(parts, path+"="+hashes[i])
		}
	}
	return sparkwing.Key(parts...), nil
}

func trackedFiles(runContext context.Context, directory string, globs []string) ([]string, error) {
	arguments := append([]string{"ls-files", "--"}, globs...)
	return sparkwing.Exec(runContext, "git", arguments...).Dir(directory).Lines()
}

// onDisk excludes unstaged deletions. Other inspection failures preserve the error.
func onDisk(directory string, paths []string) ([]string, error) {
	kept := paths[:0:0]
	for _, path := range paths {
		_, err := os.Lstat(filepath.Join(directory, path))
		switch {
		case err == nil:
			kept = append(kept, path)
		case errors.Is(err, fs.ErrNotExist):
			// SAFETY: An unstaged deletion changes the key by removing its path.
		default:
			return nil, fmt.Errorf("stat tracked file: %w", err)
		}
	}
	return kept, nil
}

func hashObjects(runContext context.Context, directory string, paths []string) ([]string, error) {
	// SAFETY: Bound each argument list to fit the operating system execution limit.
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
		arguments := append([]string{"hash-object", "--"}, paths[start:end]...)
		batch, err := sparkwing.Exec(runContext, "git", arguments...).Dir(directory).Lines()
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, batch...)
		start = end
	}
	return hashes, nil
}

// changedVsBase reports known=false when baseRef cannot be resolved.
func changedVsBase(runContext context.Context, directory, baseRef string, globs []string) (changed, known bool, err error) {
	if _, resolveError := sparkwing.Exec(runContext, "git", "rev-parse", "--verify", "--quiet", baseRef+"^{commit}").Dir(directory).String(); resolveError != nil {
		return false, false, nil
	}
	arguments := append([]string{"diff", "--quiet", baseRef, "--"}, globs...)
	_, diffError := sparkwing.Exec(runContext, "git", arguments...).Dir(directory).Capture()
	if diffError == nil {
		return false, true, nil
	}
	var exitErr *sparkwing.ExecError
	if errors.As(diffError, &exitErr) && exitErr.ExitCode == 1 {
		return true, true, nil
	}
	return false, false, diffError
}
