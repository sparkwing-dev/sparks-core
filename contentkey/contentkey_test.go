package contentkey

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing/sparkwingtest"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

type repo struct {
	t         *testing.T
	directory string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	directory := t.TempDir()
	repository := &repo{t: t, directory: directory}
	repository.git("init", "-q")
	repository.git("config", "user.email", "test@example.com")
	repository.git("config", "user.name", "Test")
	repository.git("config", "commit.gpgsign", "false")
	return repository
}

func (repository *repo) git(args ...string) string {
	repository.t.Helper()
	command := exec.CommandContext(repository.t.Context(), "git", args...)
	command.Dir = repository.directory
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	output, err := command.CombinedOutput()
	if !sameKey(err, nil) {
		repository.t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func (repository *repo) write(relativePath, content string) {
	repository.t.Helper()
	path := filepath.Join(repository.directory, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		repository.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		repository.t.Fatal(err)
	}
}

func (repository *repo) commitAll(message string) {
	repository.t.Helper()
	repository.git("add", "-A")
	repository.git("commit", "-q", "-m", message)
}

func mustKey(t *testing.T, directory, salt string, globs []string) sparkwing.MemoValue {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Helper()
	key, err := contentKey(host.Context(t.Context()), directory, salt, globs)
	if !sameKey(err, nil) {
		t.Fatalf("contentKey: %v", err)
	}
	return key
}

func TestContentKey_StableForIdenticalContent(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.write("go.mod", "module x\n")
	repository.commitAll("init")

	globs := []string{"*.go", "go.mod"}
	first := mustKey(t, repository.directory, "", globs)
	second := mustKey(t, repository.directory, "", globs)
	if !sameKey(first, second) {
		t.Fatalf("key not stable: %q != %q", first, second)
	}
	if sameKey(first, nil) {
		t.Fatalf("expected a real key, got %q", first)
	}
}

func TestContentKey_ChangesWhenFileContentChanges(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, repository.directory, "", globs)

	repository.write("main.go", "package main // changed\n")
	after := mustKey(t, repository.directory, "", globs)
	if sameKey(before, after) {
		t.Fatalf("key did not change after edit: %q", before)
	}
}

func TestContentKey_HashesUncommittedWorkingTree(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	globs := []string{"*.go"}
	committed := mustKey(t, repository.directory, "", globs)

	repository.write("main.go", "package main // dirty\n")
	dirty := mustKey(t, repository.directory, "", globs)
	if sameKey(committed, dirty) {
		t.Fatalf("uncommitted edit not reflected in key: %q", committed)
	}
}

func TestContentKey_SaltChangesKey(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	globs := []string{"*.go"}

	base := mustKey(t, repository.directory, "", globs)
	v1 := mustKey(t, repository.directory, "v1", globs)
	v2 := mustKey(t, repository.directory, "v2", globs)
	if sameKey(v1, base) || sameKey(v2, base) || sameKey(v1, v2) {
		t.Fatalf("salt not distinguishing keys: base=%q v1=%q v2=%q", base, v1, v2)
	}
	if again := mustKey(t, repository.directory, "v1", globs); !sameKey(again, v1) {
		t.Fatalf("salted key not stable: %q != %q", again, v1)
	}
}

func TestContentKey_IgnoresUntrackedAndGitignored(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.write(".gitignore", "*.log\n")
	repository.commitAll("init")
	globs := []string{"*.go", "*.log"}
	before := mustKey(t, repository.directory, "", globs)

	repository.write("debug.log", "noise\n")
	repository.write("scratch.go", "package scratch\n")
	after := mustKey(t, repository.directory, "", globs)
	if !sameKey(before, after) {
		t.Fatalf("untracked/ignored files changed key: %q != %q", before, after)
	}
}

func TestContentKey_ScopedByGlobs(t *testing.T) {
	repository := newRepo(t)
	repository.write("app/main.go", "package main\n")
	repository.write("docs/readme.md", "hi\n")
	repository.commitAll("init")
	globs := []string{"app/*.go"}
	before := mustKey(t, repository.directory, "", globs)

	repository.write("docs/readme.md", "changed\n")
	repository.commitAll("docs")
	after := mustKey(t, repository.directory, "", globs)
	if !sameKey(before, after) {
		t.Fatalf("change outside globs changed the key: %q != %q", before, after)
	}

	repository.write("app/main.go", "package main // v2\n")
	repository.commitAll("app")
	scoped := mustKey(t, repository.directory, "", globs)
	if sameKey(scoped, before) {
		t.Fatalf("change inside globs did not change the key: %q", scoped)
	}
}

func TestContentKey_RenameChangesKey(t *testing.T) {
	repository := newRepo(t)
	repository.write("a.go", "package p\n")
	repository.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, repository.directory, "", globs)

	repository.git("mv", "a.go", "b.go")
	repository.commitAll("rename")
	after := mustKey(t, repository.directory, "", globs)
	if sameKey(before, after) {
		t.Fatalf("rename (same content, new path) did not change key: %q", before)
	}
}

func TestContentKey_EmptyMatchIsStableNotError(t *testing.T) {
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	globs := []string{"*.rs"}

	first := mustKey(t, repository.directory, "", globs)
	second := mustKey(t, repository.directory, "", globs)
	if !sameKey(first, second) {
		t.Fatalf("empty-match key not stable: %q != %q", first, second)
	}
	if sameKey(first, nil) {
		t.Fatalf("empty match should still yield a deterministic key, got %q", first)
	}
}

func TestContentKey_LargeFileSetChunksArgv(t *testing.T) {
	repository := newRepo(t)
	const fileCount = 4000
	for i := 0; i < fileCount; i++ {
		repository.write(filepath.Join("pkg", padName(i)+".go"), "package p\n")
	}
	repository.commitAll("init")
	globs := []string{"pkg/*.go"}

	first := mustKey(t, repository.directory, "", globs)
	second := mustKey(t, repository.directory, "", globs)
	if !sameKey(first, second) {
		t.Fatalf("large-set key not stable: %q != %q", first, second)
	}
	if sameKey(first, nil) {
		t.Fatalf("large set should hash to a real key, got %q", first)
	}

	repository.write(filepath.Join("pkg", padName(fileCount/2)+".go"), "package p // changed\n")
	after := mustKey(t, repository.directory, "", globs)
	if sameKey(after, first) {
		t.Fatalf("editing one file in a chunked set did not change the key: %q", first)
	}
}

func TestContentKey_DeletedTrackedFileDropsFromKey(t *testing.T) {
	repository := newRepo(t)
	repository.write("a.go", "package p\n")
	repository.write("b.go", "package p\n")
	repository.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, repository.directory, "", globs)

	if err := os.Remove(filepath.Join(repository.directory, "b.go")); err != nil {
		t.Fatal(err)
	}
	after := mustKey(t, repository.directory, "", globs)
	if sameKey(after, nil) {
		t.Fatalf("unstaged deletion returned an unusable key %q", after)
	}
	if sameKey(after, before) {
		t.Fatalf("deleting a tracked file did not change the key: %q", before)
	}
}

// lockDir makes child inspection fail with a permission error.
func lockDir(t *testing.T, directory string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission-based Lstat failure not constructible on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	if err := os.Chmod(directory, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})
}

func TestContentKey_StatFailureErrorsInsteadOfChangingKey(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("locked/a.go", "package p\n")
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, repository.directory, "", globs)

	locked := filepath.Join(repository.directory, "locked")
	lockDir(t, locked)
	_, err := contentKey(host.Context(t.Context()), repository.directory, "", globs)
	if sameKey(err, nil) {
		t.Fatal("a stat failure that is not a deletion must error, not shrink the key")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("permission failure misclassified as not-exist: %v", err)
	}

	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	after := mustKey(t, repository.directory, "", globs)
	if !sameKey(after, before) {
		t.Fatalf("key changed across a transient stat failure: %q != %q", before, after)
	}
}

func padName(i int) string {
	return fmt.Sprintf("file_%08d_with_a_deliberately_long_suffix_to_grow_argv", i)
}

func TestChangedVsBase_CleanTreeIsUnchanged(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")

	changed, known, err := changedVsBase(host.Context(t.Context()), repository.directory, "HEAD", []string{"*.go"})
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("base HEAD should be known")
	}
	if changed {
		t.Fatal("clean tree should report unchanged")
	}
}

func TestChangedVsBase_WorkingTreeEditIsChanged(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	repository.write("main.go", "package main // edit\n")

	changed, known, err := changedVsBase(host.Context(t.Context()), repository.directory, "HEAD", []string{"*.go"})
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if !known || !changed {
		t.Fatalf("edited tree: want changed+known, got changed=%v known=%v", changed, known)
	}
}

func TestChangedVsBase_ScopedToGlobs(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("app/main.go", "package main\n")
	repository.write("docs/readme.md", "hi\n")
	repository.commitAll("init")
	repository.write("docs/readme.md", "changed\n")

	changed, known, err := changedVsBase(host.Context(t.Context()), repository.directory, "HEAD", []string{"app"})
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if !known {
		t.Fatal("base should be known")
	}
	if changed {
		t.Fatal("edit outside watched paths should report unchanged")
	}
}

func TestChangedVsBase_MissingBaseIsUnknown(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")

	changed, known, err := changedVsBase(host.Context(t.Context()), repository.directory, "origin/does-not-exist", []string{"*.go"})
	if !sameKey(err, nil) {
		t.Fatalf("missing base should not error, got %v", err)
	}
	if known {
		t.Fatal("missing base ref should be reported unknown")
	}
	if changed {
		t.Fatal("unknown base should not claim changed")
	}
}

func TestUnchanged_Predicate(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	t.Chdir(repository.directory)

	if !Unchanged("HEAD", "*.go")(host.Context(t.Context())) {
		t.Fatal("clean tree vs HEAD should skip (unchanged=true)")
	}
	repository.write("main.go", "package main // edit\n")
	if Unchanged("HEAD", "*.go")(host.Context(t.Context())) {
		t.Fatal("edited tree should not skip")
	}
}

func TestUnchanged_MissingBaseDoesNotSkip(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	t.Chdir(repository.directory)

	if Unchanged("origin/nope", "*.go")(host.Context(t.Context())) {
		t.Fatal("missing base must return unchanged=false")
	}
}

func TestChanged_IsInverseOfUnchanged(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := newRepo(t)
	repository.write("main.go", "package main\n")
	repository.commitAll("init")
	t.Chdir(repository.directory)

	ctx := host.Context(t.Context())
	if Changed("HEAD", "*.go")(ctx) {
		t.Fatal("clean tree should report not-changed")
	}
	repository.write("main.go", "package main // edit\n")
	if !Changed("HEAD", "*.go")(ctx) {
		t.Fatal("edited tree should report changed")
	}
}

func goModuleRepo(t *testing.T) *repo {
	t.Helper()
	t.Setenv("GOWORK", "off")
	repository := newRepo(t)
	repository.write("go.mod", "module testmod\n\ngo 1.26\n")
	repository.write("lib/lib.go", "package lib\n\nfunc Hello() string { return \"hi\" }\n")
	repository.write("app/app.go", "package app\n\nimport \"testmod/lib\"\n\nfunc Greet() string { return lib.Hello() }\n")
	repository.write("app/app_test.go", "package app\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")
	repository.commitAll("init")
	return repository
}

func hasPath(paths []string, want string) bool {
	for _, path := range paths {
		if sameKey(path, want) {
			return true
		}
	}
	return false
}

func TestGoDeps_IncludesTargetSourceTestsAndSameModuleDeps(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := goModuleRepo(t)
	files, err := GoDeps(host.Context(t.Context()), repository.directory, "./app")
	if !sameKey(err, nil) {
		t.Fatalf("GoDeps: %v", err)
	}
	for _, want := range []string{"app/app.go", "app/app_test.go", "lib/lib.go"} {
		if !hasPath(files, want) {
			t.Errorf("GoDeps(./app) missing %q; got %v", want, files)
		}
	}
	if hasPath(files, "go.mod") {
		t.Errorf("GoDeps must not fold go.mod into the closure (it is not a package file); got %v", files)
	}
}

func TestGoDeps_ExcludesDependencyTestFiles(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := goModuleRepo(t)
	repository.write("lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif Hello() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")
	repository.commitAll("lib test")

	appDeps, err := GoDeps(host.Context(t.Context()), repository.directory, "./app")
	if !sameKey(err, nil) {
		t.Fatalf("GoDeps(./app): %v", err)
	}
	if hasPath(appDeps, "lib/lib_test.go") {
		t.Errorf("a dependency's test file must not enter the target's closure; got %v", appDeps)
	}

	libDeps, err := GoDeps(host.Context(t.Context()), repository.directory, "./lib")
	if !sameKey(err, nil) {
		t.Fatalf("GoDeps(./lib): %v", err)
	}
	if !hasPath(libDeps, "lib/lib_test.go") {
		t.Errorf("a package's own test file must enter its own closure; got %v", libDeps)
	}
}

func TestSaltedGoPackage_ChangesWhenDependencyChanges(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := goModuleRepo(t)
	t.Chdir(repository.directory)
	ctx := host.Context(t.Context())

	before, err := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if sameKey(before, nil) {
		t.Fatalf("expected a real key, got NoCache")
	}

	repository.write("lib/lib.go", "package lib\n\nfunc Hello() string { return \"changed\" }\n")
	afterDep, err := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if sameKey(afterDep, before) {
		t.Fatalf("editing a same-module dependency must change the package key")
	}
}

func TestSaltedGoPackage_UnaffectedByUnrelatedPackage(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := goModuleRepo(t)
	repository.write("other/other.go", "package other\n\nfunc Noop() {}\n")
	repository.commitAll("other")
	t.Chdir(repository.directory)
	ctx := host.Context(t.Context())

	before, err := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	repository.write("other/other.go", "package other\n\nfunc Noop() { _ = 1 }\n")
	after, err := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if !sameKey(after, before) {
		t.Fatalf("editing a package outside the closure must not change the key")
	}
}

func TestSaltedGoPackage_DistinctPerSpec(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	repository := goModuleRepo(t)
	t.Chdir(repository.directory)
	ctx := host.Context(t.Context())

	app, err := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	lib, err := SaltedGoPackage("v1", "./lib", "go.mod")(ctx)
	if !sameKey(err, nil) {
		t.Fatal(err)
	}
	if sameKey(app, lib) {
		t.Fatalf("distinct package specs must yield distinct keys: %q", app)
	}
}

func TestOfGoPackage_ErrorsOutsideModule(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("GOWORK", "off")
	directory := t.TempDir()
	t.Chdir(directory)
	key, err := OfGoPackage("./app")(host.Context(t.Context()))
	if key != nil || err == nil {
		t.Fatalf("outside-module resolution = %q, %v; want empty key and error", key, err)
	}
}

func sameKey(left, right sparkwing.MemoValue) bool {
	return reflect.DeepEqual(left, right)
}
