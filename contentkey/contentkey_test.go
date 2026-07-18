package contentkey

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// repo is a throwaway git repository rooted at a temp dir.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	dir := t.TempDir()
	r := &repo{t: t, dir: dir}
	r.git("init", "-q")
	r.git("config", "user.email", "test@example.com")
	r.git("config", "user.name", "Test")
	r.git("config", "commit.gpgsign", "false")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (r *repo) write(rel, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commitAll(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

func mustKey(t *testing.T, dir, salt string, globs []string) sparkwing.CacheKey {
	t.Helper()
	k, err := contentKey(context.Background(), dir, salt, globs)
	if err != nil {
		t.Fatalf("contentKey: %v", err)
	}
	return k
}

func TestContentKey_StableForIdenticalContent(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.write("go.mod", "module x\n")
	r.commitAll("init")

	globs := []string{"*.go", "go.mod"}
	first := mustKey(t, r.dir, "", globs)
	second := mustKey(t, r.dir, "", globs)
	if first != second {
		t.Fatalf("key not stable: %q != %q", first, second)
	}
	if first == "" || first.IsNoCache() {
		t.Fatalf("expected a real key, got %q", first)
	}
}

func TestContentKey_ChangesWhenFileContentChanges(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, r.dir, "", globs)

	r.write("main.go", "package main // changed\n")
	after := mustKey(t, r.dir, "", globs)
	if before == after {
		t.Fatalf("key did not change after edit: %q", before)
	}
}

func TestContentKey_HashesUncommittedWorkingTree(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	globs := []string{"*.go"}
	committed := mustKey(t, r.dir, "", globs)

	r.write("main.go", "package main // dirty\n")
	dirty := mustKey(t, r.dir, "", globs)
	if committed == dirty {
		t.Fatalf("uncommitted edit not reflected in key: %q", committed)
	}
}

func TestContentKey_SaltChangesKey(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	globs := []string{"*.go"}

	base := mustKey(t, r.dir, "", globs)
	v1 := mustKey(t, r.dir, "v1", globs)
	v2 := mustKey(t, r.dir, "v2", globs)
	if v1 == base || v2 == base || v1 == v2 {
		t.Fatalf("salt not distinguishing keys: base=%q v1=%q v2=%q", base, v1, v2)
	}
	if again := mustKey(t, r.dir, "v1", globs); again != v1 {
		t.Fatalf("salted key not stable: %q != %q", again, v1)
	}
}

func TestContentKey_IgnoresUntrackedAndGitignored(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.write(".gitignore", "*.log\n")
	r.commitAll("init")
	globs := []string{"*.go", "*.log"}
	before := mustKey(t, r.dir, "", globs)

	r.write("debug.log", "noise\n")
	r.write("scratch.go", "package scratch\n")
	after := mustKey(t, r.dir, "", globs)
	if before != after {
		t.Fatalf("untracked/ignored files changed key: %q != %q", before, after)
	}
}

func TestContentKey_ScopedByGlobs(t *testing.T) {
	r := newRepo(t)
	r.write("app/main.go", "package main\n")
	r.write("docs/readme.md", "hi\n")
	r.commitAll("init")
	globs := []string{"app/*.go"}
	before := mustKey(t, r.dir, "", globs)

	r.write("docs/readme.md", "changed\n")
	r.commitAll("docs")
	after := mustKey(t, r.dir, "", globs)
	if before != after {
		t.Fatalf("change outside globs changed the key: %q != %q", before, after)
	}

	r.write("app/main.go", "package main // v2\n")
	r.commitAll("app")
	scoped := mustKey(t, r.dir, "", globs)
	if scoped == before {
		t.Fatalf("change inside globs did not change the key: %q", scoped)
	}
}

func TestContentKey_RenameChangesKey(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package p\n")
	r.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, r.dir, "", globs)

	r.git("mv", "a.go", "b.go")
	r.commitAll("rename")
	after := mustKey(t, r.dir, "", globs)
	if before == after {
		t.Fatalf("rename (same content, new path) did not change key: %q", before)
	}
}

func TestContentKey_EmptyMatchIsStableNotError(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	globs := []string{"*.rs"}

	first := mustKey(t, r.dir, "", globs)
	second := mustKey(t, r.dir, "", globs)
	if first != second {
		t.Fatalf("empty-match key not stable: %q != %q", first, second)
	}
	if first == "" || first.IsNoCache() {
		t.Fatalf("empty match should still yield a deterministic key, got %q", first)
	}
}

// TestContentKey_LargeFileSetChunksArgv uses enough long-named files that a
// single argv would blow past the per-exec byte budget, forcing hashObjects to
// batch. It checks the key is stable and still reflects a one-file edit.
func TestContentKey_LargeFileSetChunksArgv(t *testing.T) {
	r := newRepo(t)
	const n = 4000
	for i := 0; i < n; i++ {
		r.write(filepath.Join("pkg", padName(i)+".go"), "package p\n")
	}
	r.commitAll("init")
	globs := []string{"pkg/*.go"}

	first := mustKey(t, r.dir, "", globs)
	second := mustKey(t, r.dir, "", globs)
	if first != second {
		t.Fatalf("large-set key not stable: %q != %q", first, second)
	}
	if first == "" || first.IsNoCache() {
		t.Fatalf("large set should hash to a real key, got %q", first)
	}

	r.write(filepath.Join("pkg", padName(n/2)+".go"), "package p // changed\n")
	after := mustKey(t, r.dir, "", globs)
	if after == first {
		t.Fatalf("editing one file in a chunked set did not change the key: %q", first)
	}
}

// TestContentKey_DeletedTrackedFileDropsFromKey removes a tracked file from the
// working tree without staging the removal, so `git ls-files` still lists it.
// The key must still compute (not degrade to NoCache) and must change to
// reflect the deletion.
func TestContentKey_DeletedTrackedFileDropsFromKey(t *testing.T) {
	r := newRepo(t)
	r.write("a.go", "package p\n")
	r.write("b.go", "package p\n")
	r.commitAll("init")
	globs := []string{"*.go"}
	before := mustKey(t, r.dir, "", globs)

	if err := os.Remove(filepath.Join(r.dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	after := mustKey(t, r.dir, "", globs)
	if after == "" || after.IsNoCache() {
		t.Fatalf("deleted-but-tracked file should not bust the key to NoCache, got %q", after)
	}
	if after == before {
		t.Fatalf("deleting a tracked file did not change the key: %q", before)
	}
}

func padName(i int) string {
	return fmt.Sprintf("file_%08d_with_a_deliberately_long_suffix_to_grow_argv", i)
}

func TestChangedVsBase_CleanTreeIsUnchanged(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")

	changed, known, err := changedVsBase(context.Background(), r.dir, "HEAD", []string{"*.go"})
	if err != nil {
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
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	r.write("main.go", "package main // edit\n")

	changed, known, err := changedVsBase(context.Background(), r.dir, "HEAD", []string{"*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !known || !changed {
		t.Fatalf("edited tree: want changed+known, got changed=%v known=%v", changed, known)
	}
}

func TestChangedVsBase_ScopedToGlobs(t *testing.T) {
	r := newRepo(t)
	r.write("app/main.go", "package main\n")
	r.write("docs/readme.md", "hi\n")
	r.commitAll("init")
	r.write("docs/readme.md", "changed\n")

	changed, known, err := changedVsBase(context.Background(), r.dir, "HEAD", []string{"app"})
	if err != nil {
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
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")

	changed, known, err := changedVsBase(context.Background(), r.dir, "origin/does-not-exist", []string{"*.go"})
	if err != nil {
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
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	sparkwing.SetWorkDir(r.dir)

	if !Unchanged("HEAD", "*.go")(context.Background()) {
		t.Fatal("clean tree vs HEAD should skip (unchanged=true)")
	}
	r.write("main.go", "package main // edit\n")
	if Unchanged("HEAD", "*.go")(context.Background()) {
		t.Fatal("edited tree should not skip")
	}
}

func TestUnchanged_MissingBaseFailsSafe(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	sparkwing.SetWorkDir(r.dir)

	if Unchanged("origin/nope", "*.go")(context.Background()) {
		t.Fatal("missing base must fail safe to run (unchanged=false)")
	}
}

func TestChanged_IsInverseOfUnchanged(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	sparkwing.SetWorkDir(r.dir)

	ctx := context.Background()
	if Changed("HEAD", "*.go")(ctx) {
		t.Fatal("clean tree should report not-changed")
	}
	r.write("main.go", "package main // edit\n")
	if !Changed("HEAD", "*.go")(ctx) {
		t.Fatal("edited tree should report changed")
	}
}

// goModuleRepo builds a two-package git repo module (testmod) where
// package app imports package lib, plus app's test. It is the minimal
// shape GoDeps must reason about: a target package, a same-module
// dependency, and test files.
func goModuleRepo(t *testing.T) *repo {
	t.Helper()
	t.Setenv("GOWORK", "off")
	r := newRepo(t)
	r.write("go.mod", "module testmod\n\ngo 1.26\n")
	r.write("lib/lib.go", "package lib\n\nfunc Hello() string { return \"hi\" }\n")
	r.write("app/app.go", "package app\n\nimport \"testmod/lib\"\n\nfunc Greet() string { return lib.Hello() }\n")
	r.write("app/app_test.go", "package app\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")
	r.commitAll("init")
	return r
}

func hasPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want {
			return true
		}
	}
	return false
}

func TestGoDeps_IncludesTargetSourceTestsAndSameModuleDeps(t *testing.T) {
	r := goModuleRepo(t)
	files, err := GoDeps(context.Background(), r.dir, "./app")
	if err != nil {
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
	r := goModuleRepo(t)
	r.write("lib/lib_test.go", "package lib\n\nimport \"testing\"\n\nfunc TestHello(t *testing.T) {\n\tif Hello() == \"\" {\n\t\tt.Fatal(\"empty\")\n\t}\n}\n")
	r.commitAll("lib test")

	appDeps, err := GoDeps(context.Background(), r.dir, "./app")
	if err != nil {
		t.Fatalf("GoDeps(./app): %v", err)
	}
	if hasPath(appDeps, "lib/lib_test.go") {
		t.Errorf("a dependency's test file must not enter the target's closure; got %v", appDeps)
	}

	libDeps, err := GoDeps(context.Background(), r.dir, "./lib")
	if err != nil {
		t.Fatalf("GoDeps(./lib): %v", err)
	}
	if !hasPath(libDeps, "lib/lib_test.go") {
		t.Errorf("a package's own test file must enter its own closure; got %v", libDeps)
	}
}

func TestSaltedGoPackage_BustsWhenDependencyChanges(t *testing.T) {
	r := goModuleRepo(t)
	sparkwing.SetWorkDir(r.dir)
	ctx := context.Background()

	before := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if before.IsNoCache() {
		t.Fatalf("expected a real key, got NoCache")
	}

	r.write("lib/lib.go", "package lib\n\nfunc Hello() string { return \"changed\" }\n")
	afterDep := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if afterDep == before {
		t.Fatalf("editing a same-module dependency must bust the package key")
	}
}

func TestSaltedGoPackage_UnaffectedByUnrelatedPackage(t *testing.T) {
	r := goModuleRepo(t)
	r.write("other/other.go", "package other\n\nfunc Noop() {}\n")
	r.commitAll("other")
	sparkwing.SetWorkDir(r.dir)
	ctx := context.Background()

	before := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	r.write("other/other.go", "package other\n\nfunc Noop() { _ = 1 }\n")
	after := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	if after != before {
		t.Fatalf("editing a package outside the closure must not change the key")
	}
}

func TestSaltedGoPackage_DistinctPerSpec(t *testing.T) {
	r := goModuleRepo(t)
	sparkwing.SetWorkDir(r.dir)
	ctx := context.Background()

	app := SaltedGoPackage("v1", "./app", "go.mod")(ctx)
	lib := SaltedGoPackage("v1", "./lib", "go.mod")(ctx)
	if app == lib {
		t.Fatalf("distinct package specs must yield distinct keys: %q", app)
	}
}

func TestOfGoPackage_NoCacheOutsideModule(t *testing.T) {
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	sparkwing.SetWorkDir(dir)
	key := OfGoPackage("./app")(context.Background())
	if !key.IsNoCache() {
		t.Fatalf("outside a Go module OfGoPackage should yield NoCache, got %q", key)
	}
}

func TestOfPaths_NoCacheOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	sparkwing.SetWorkDir(dir)

	key := OfPaths("*.go")(context.Background())
	if !key.IsNoCache() {
		t.Fatalf("outside a git repo OfPaths should yield NoCache, got %q", key)
	}
}

func TestOfPaths_WiresThroughWorkDir(t *testing.T) {
	r := newRepo(t)
	r.write("main.go", "package main\n")
	r.commitAll("init")
	sparkwing.SetWorkDir(r.dir)

	viaExport := OfPaths("*.go")(context.Background())
	viaCore := mustKey(t, r.dir, "", []string{"*.go"})
	if viaExport != viaCore {
		t.Fatalf("exported OfPaths disagrees with core: %q != %q", viaExport, viaCore)
	}
}
