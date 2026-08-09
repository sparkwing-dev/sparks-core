package jobs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// The sweeps read this file too, so the fixtures spell their patterns with an
// escape and a join rather than literally. Written out they are the real
// thing; read as source, neither trips the check under test.
const (
	sweepEmDash    = "\u2014"
	sweepTrackerID = "TOD" + "-42"
)

// sweepGit runs git in dir with signing and identity forced, so the fixture
// does not depend on the machine's git config.
func sweepGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.name=gate",
		"-c", "user.email=gate@example.com",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// sweepWrite writes content at path, creating parent directories.
func sweepWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sweepFixtureRepo builds a git repo whose history holds history, commits it,
// and points the gate at the repo for the duration of the test. A real commit
// is what gives the staged diff a HEAD to compare against.
func sweepFixtureRepo(t *testing.T, history string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	sweepGit(t, root, "init", "-q")
	sweepWrite(t, filepath.Join(root, "NOTES.md"), history)
	sweepGit(t, root, "add", "-A")
	sweepGit(t, root, "commit", "-q", "-m", "history")
	prev := sparkwing.WorkDir()
	sparkwing.SetWorkDir(root)
	t.Cleanup(func() { sparkwing.SetWorkDir(prev) })
	return root
}

// A commit is judged on what it changes. The sweeps once read the whole
// tracked tree, so history nobody touched could refuse an unrelated commit.
func TestRegexSweepsIgnoreAFileTheCommitDoesNotTouch(t *testing.T) {
	root := sweepFixtureRepo(t, "a dash "+sweepEmDash+" and a "+sweepTrackerID+" id\n")
	ctx := context.Background()

	sweepWrite(t, filepath.Join(root, "internal", "clean.go"),
		"package internal\n\nfunc Clean() int { return 2 }\n")
	sweepGit(t, root, "add", "-A")

	if err := checkEmDashes(ctx); err != nil {
		t.Errorf("em-dash sweep charged the commit for untouched history: %v", err)
	}
	if err := checkTrackerIDs(ctx); err != nil {
		t.Errorf("tracker-id sweep charged the commit for untouched history: %v", err)
	}
}

// The narrowing must not disarm the sweeps: content the commit actually
// introduces is still refused.
func TestRegexSweepsRefuseWhatTheStagedChangeIntroduces(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		check func(context.Context) error
	}{
		{"em dash", "package internal\n\n// Note " + sweepEmDash + " here.\nfunc Bad() int { return 3 }\n", checkEmDashes},
		{"tracker id", "package internal\n\n// See " + sweepTrackerID + ".\nfunc Bad() int { return 3 }\n", checkTrackerIDs},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := sweepFixtureRepo(t, "clean history\n")

			sweepWrite(t, filepath.Join(root, "internal", "bad.go"), tc.body)
			sweepGit(t, root, "add", "-A")

			if err := tc.check(context.Background()); err == nil {
				t.Fatal("the sweep passed a staged change that introduces the pattern")
			}
		})
	}
}

// The whole-tree audit is how pre-existing drift still gets found, off the
// critical path of an unrelated commit.
func TestRegexSweepAllReadsPastTheStagedChange(t *testing.T) {
	root := sweepFixtureRepo(t, "a dash "+sweepEmDash+" here\n")
	ctx := context.Background()

	sweepWrite(t, filepath.Join(root, "internal", "clean.go"),
		"package internal\n\nfunc Clean() int { return 2 }\n")
	sweepGit(t, root, "add", "-A")

	t.Setenv("SPARKWING_REGEX_SWEEP_ALL", "1")
	if err := checkEmDashes(ctx); err == nil {
		t.Fatal("the whole-tree audit missed an em dash outside the staged change")
	}
}
