package release

import (
	"context"
	"strings"
	"testing"
)

func TestGhArgs(t *testing.T) {
	got := ghArgs(GitHubReleaseConfig{
		Tag:       "v1.2.3",
		Title:     "Release 1.2.3",
		NotesFile: "notes.md",
		Assets:    []string{"dist/app_linux", "dist/checksums.txt"},
		Repo:      "owner/repo",
	})
	want := "release create v1.2.3 --repo owner/repo --title Release 1.2.3 --notes-file notes.md dist/app_linux dist/checksums.txt"
	if strings.Join(got, " ") != want {
		t.Errorf("ghArgs = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestGhArgs_DefaultsTitleToTagAndInlineNotes(t *testing.T) {
	got := ghArgs(GitHubReleaseConfig{Tag: "v2.0.0", Notes: "hi", Draft: true, Prerelease: true})
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "--title v2.0.0") {
		t.Errorf("expected title defaulted to tag: %q", joined)
	}
	if !strings.Contains(joined, "--notes hi") {
		t.Errorf("expected inline notes: %q", joined)
	}
	if !strings.Contains(joined, "--draft") || !strings.Contains(joined, "--prerelease") {
		t.Errorf("expected draft/prerelease flags: %q", joined)
	}
}

func TestNpmArgs(t *testing.T) {
	got := npmArgs(NpmPublishConfig{Registry: "https://r.example", Access: "public", Tag: "next", Provenance: true})
	want := "publish --registry https://r.example --access public --tag next --provenance"
	if strings.Join(got, " ") != want {
		t.Errorf("npmArgs = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestNpmArgs_Minimal(t *testing.T) {
	got := npmArgs(NpmPublishConfig{})
	if strings.Join(got, " ") != "publish" {
		t.Errorf("npmArgs = %q, want %q", strings.Join(got, " "), "publish")
	}
}

func TestPyPIArgs_TwineDefault(t *testing.T) {
	name, args := pypiArgs("twine", PyPIPublishConfig{Repository: "testpypi"})
	if name != "twine" {
		t.Errorf("tool = %q, want twine", name)
	}
	want := "upload --repository testpypi dist/*"
	if strings.Join(args, " ") != want {
		t.Errorf("pypiArgs = %q, want %q", strings.Join(args, " "), want)
	}
}

func TestPyPIArgs_Uv(t *testing.T) {
	name, args := pypiArgs("uv", PyPIPublishConfig{Dist: "dist/pkg.whl"})
	if name != "uv" {
		t.Errorf("tool = %q, want uv", name)
	}
	if strings.Join(args, " ") != "publish dist/pkg.whl" {
		t.Errorf("pypiArgs = %q, want %q", strings.Join(args, " "), "publish dist/pkg.whl")
	}
}

func TestPyPIPublish_RejectsUnknownTool(t *testing.T) {
	err := PyPIPublish(context.Background(), PyPIPublishConfig{Tool: "poetry"})
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// The dry-run tests assert echo-and-skip: with SPARKWING_DRY_RUN set, a
// publish must return nil without executing a (here nonexistent) binary.
func TestGitHubRelease_DryRunSkipsExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := GitHubRelease(context.Background(), GitHubReleaseConfig{Tag: "v1.0.0"}); err != nil {
		t.Errorf("dry-run GitHubRelease should be a no-op, got %v", err)
	}
}

func TestGitHubRelease_DryRunViaConfig(t *testing.T) {
	if err := GitHubRelease(context.Background(), GitHubReleaseConfig{Tag: "v1.0.0", DryRun: true}); err != nil {
		t.Errorf("cfg.DryRun GitHubRelease should be a no-op, got %v", err)
	}
}

func TestGitHubRelease_RequiresTag(t *testing.T) {
	if err := GitHubRelease(context.Background(), GitHubReleaseConfig{}); err == nil {
		t.Fatal("expected error when Tag is empty")
	}
}

func TestNpmPublish_DryRunSkipsExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := NpmPublish(context.Background(), NpmPublishConfig{}); err != nil {
		t.Errorf("dry-run NpmPublish should be a no-op, got %v", err)
	}
}

func TestPyPIPublish_DryRunSkipsExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := PyPIPublish(context.Background(), PyPIPublishConfig{}); err != nil {
		t.Errorf("dry-run PyPIPublish should be a no-op, got %v", err)
	}
}
