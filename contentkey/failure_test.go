package contentkey

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func TestCacheResolversPreserveFailures(t *testing.T) {
	previous := sparkwing.WorkDir()
	sparkwing.SetWorkDir(t.TempDir())
	t.Cleanup(func() { sparkwing.SetWorkDir(previous) })
	resolvers := map[string]any{
		"paths":          OfPaths("*.go"),
		"salted paths":   Salted("example", "*.go"),
		"package":        OfGoPackage("./example"),
		"salted package": SaltedGoPackage("example", "./example"),
	}
	for name, candidate := range resolvers {
		t.Run(name, func(t *testing.T) {
			resolver, ok := candidate.(sparkwing.CacheKeyFn)
			if !ok {
				t.Fatalf("resolver type = %T, want sparkwing.CacheKeyFn with an error result", candidate)
			}
			t.Run("cancelled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				key, err := resolver(ctx)
				if key != "" || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled resolution = %q, %v; want empty key and cancellation cause", key, err)
				}
			})
			t.Run("missing tool", func(t *testing.T) {
				t.Setenv("PATH", t.TempDir())
				key, err := resolver(t.Context())
				var toolError *exec.Error
				if key != "" || !errors.As(err, &toolError) {
					t.Fatalf("resolution without tools = %q, %v; want empty key and executable cause", key, err)
				}
			})
		})
	}
}

func TestCacheResolversRequireWorkingDirectory(t *testing.T) {
	repository := goModuleRepo(t)
	t.Chdir(repository.directory)
	setTestWorkDir(t, "")
	resolvers := map[string]sparkwing.CacheKeyFn{
		"paths":          OfPaths("*.go"),
		"salted paths":   Salted("example", "*.go"),
		"package":        OfGoPackage("./app"),
		"salted package": SaltedGoPackage("example", "./app"),
	}
	for name, resolver := range resolvers {
		t.Run(name, func(t *testing.T) {
			key, err := resolver(t.Context())
			if key != "" || err == nil || !strings.Contains(err.Error(), "working directory") {
				t.Fatalf("resolution without SDK working directory = %q, %v; want explicit directory error", key, err)
			}
		})
	}
}

func TestGoPackageRequiresMainModule(t *testing.T) {
	repository := goModuleRepo(t)
	setTestWorkDir(t, repository.directory)
	files, err := GoDeps(t.Context(), repository.directory, "fmt")
	if files != nil || err == nil || !strings.Contains(err.Error(), "main module") {
		t.Errorf("standard-library dependencies = %v, %v; want main module error", files, err)
	}
	key, err := SaltedGoPackage("example", "fmt")(t.Context())
	if key != "" || err == nil || !strings.Contains(err.Error(), "main module") {
		t.Errorf("standard-library key = %q, %v; want main module error", key, err)
	}
}

func TestMainModuleUsesTargetOwnership(t *testing.T) {
	dependencyRoot := filepath.Join(t.TempDir(), "dependency")
	targetRoot := filepath.Join(t.TempDir(), "target")
	packages := []goListPackage{
		{DepOnly: true, Module: &goListModule{Main: true, Dir: dependencyRoot}},
		{Module: &goListModule{Main: true, Dir: targetRoot}},
	}
	root, err := mainModuleDir(packages)
	if err != nil || root != targetRoot {
		t.Fatalf("main module = %q, %v; want target root %q", root, err, targetRoot)
	}
	packages = append(packages, goListPackage{Module: &goListModule{Main: true, Dir: dependencyRoot}})
	if root, err := mainModuleDir(packages); root != "" || err == nil {
		t.Fatalf("multiple target roots = %q, %v; want error", root, err)
	}
	if root, err := mainModuleDir([]goListPackage{{Module: &goListModule{Main: true}}}); root != "" || err == nil {
		t.Fatalf("missing main module directory = %q, %v; want error", root, err)
	}
}

func TestModuleRelativePathRejectsUnresolvedSources(t *testing.T) {
	root := filepath.Join(t.TempDir(), "module")
	cases := map[string]string{
		"outside module":     filepath.Join(t.TempDir(), "outside"),
		"relative directory": "relative-package",
	}
	for name, directory := range cases {
		t.Run(name, func(t *testing.T) {
			path, err := moduleRelativePath(root, directory, "source.go")
			if path != "" || err == nil {
				t.Fatalf("source path = %q, %v; want empty path and error", path, err)
			}
		})
	}
	path, err := moduleRelativePath(root, filepath.Join(root, "package"), "source.go")
	if err != nil || path != filepath.Join("package", "source.go") {
		t.Fatalf("source path = %q, %v; want module-relative source", path, err)
	}
}
