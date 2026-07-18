package release

import (
	"context"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// GitHubReleaseConfig configures a `gh release create` invocation.
type GitHubReleaseConfig struct {
	// Tag is the git tag the release is cut from (e.g. "v1.2.3"). Required.
	Tag string
	// Title is the release title. Defaults to Tag.
	Title string
	// Notes is the release body text, used when NotesFile is empty.
	Notes string
	// NotesFile is a path to a notes file, passed as `--notes-file`. Takes
	// precedence over Notes.
	NotesFile string
	// Assets are files uploaded with the release (built artifacts,
	// checksums), relative to the repo root.
	Assets []string
	// Repo optionally targets a specific "owner/name" via `--repo`.
	Repo string
	// Draft creates the release as a draft.
	Draft bool
	// Prerelease marks the release as a prerelease.
	Prerelease bool
	// TokenSecret is the sparkwing secret name holding the GitHub token
	// `gh` authenticates with, exported as GH_TOKEN. Empty relies on gh's
	// ambient auth.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// GitHubRelease cuts a GitHub Release with `gh release create`, uploading
// any assets and attaching the notes. Honors the dry-run convention:
// under DryRun or SPARKWING_DRY_RUN it echoes the argv and returns nil
// without reaching GitHub.
func GitHubRelease(ctx context.Context, cfg GitHubReleaseConfig) error {
	if cfg.Tag == "" {
		return fmt.Errorf("release: GitHubRelease Tag is required")
	}
	args := ghArgs(cfg)
	return step.Run(ctx, "github release ("+cfg.Tag+")", func(ctx context.Context) error {
		if cfg.DryRun || dryRun() {
			echoArgv(ctx, "gh", args)
			return nil
		}
		cmd := sparkwing.Exec(ctx, "gh", args...)
		if cfg.TokenSecret != "" {
			token, err := sparkwing.Secret(ctx, cfg.TokenSecret)
			if err != nil {
				return err
			}
			cmd = cmd.Env("GH_TOKEN", token)
		}
		_, err := cmd.Run()
		return err
	})
}

// ghArgs builds the `gh release create` argv. Pure, for testing.
func ghArgs(cfg GitHubReleaseConfig) []string {
	args := []string{"release", "create", cfg.Tag}
	if cfg.Repo != "" {
		args = append(args, "--repo", cfg.Repo)
	}
	title := cfg.Title
	if title == "" {
		title = cfg.Tag
	}
	args = append(args, "--title", title)
	if cfg.NotesFile != "" {
		args = append(args, "--notes-file", cfg.NotesFile)
	} else {
		args = append(args, "--notes", cfg.Notes)
	}
	if cfg.Draft {
		args = append(args, "--draft")
	}
	if cfg.Prerelease {
		args = append(args, "--prerelease")
	}
	return append(args, cfg.Assets...)
}

// NpmPublishConfig configures an `npm publish` invocation.
type NpmPublishConfig struct {
	// Dir is the package directory (containing package.json), relative to
	// the repo root. Defaults to ".".
	Dir string
	// Registry is the target registry URL (`--registry`). Empty uses npm's
	// configured default.
	Registry string
	// Access is the `--access` value ("public" or "restricted"). Empty
	// omits the flag.
	Access string
	// Tag is the dist-tag to publish under (`--tag`). Empty omits the flag
	// (npm defaults to "latest").
	Tag string
	// Provenance publishes with `--provenance`.
	Provenance bool
	// TokenSecret is the sparkwing secret name holding the npm auth token,
	// exported as NODE_AUTH_TOKEN.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// NpmPublish publishes a package with `npm publish`. Honors the dry-run
// convention: under DryRun or SPARKWING_DRY_RUN it echoes the argv and
// returns nil without reaching the registry.
func NpmPublish(ctx context.Context, cfg NpmPublishConfig) error {
	args := npmArgs(cfg)
	return step.Run(ctx, "npm publish", func(ctx context.Context) error {
		if cfg.DryRun || dryRun() {
			echoArgv(ctx, "npm", args)
			return nil
		}
		cmd := sparkwing.Exec(ctx, "npm", args...)
		if cfg.Dir != "" {
			cmd = cmd.Dir(cfg.Dir)
		}
		if cfg.TokenSecret != "" {
			token, err := sparkwing.Secret(ctx, cfg.TokenSecret)
			if err != nil {
				return err
			}
			cmd = cmd.Env("NODE_AUTH_TOKEN", token)
		}
		_, err := cmd.Run()
		return err
	})
}

// npmArgs builds the `npm publish` argv. Pure, for testing.
func npmArgs(cfg NpmPublishConfig) []string {
	args := []string{"publish"}
	if cfg.Registry != "" {
		args = append(args, "--registry", cfg.Registry)
	}
	if cfg.Access != "" {
		args = append(args, "--access", cfg.Access)
	}
	if cfg.Tag != "" {
		args = append(args, "--tag", cfg.Tag)
	}
	if cfg.Provenance {
		args = append(args, "--provenance")
	}
	return args
}

// PyPIPublishConfig configures a Python wheel/sdist upload.
type PyPIPublishConfig struct {
	// Dir is the working directory the upload runs from, relative to the
	// repo root. Defaults to ".".
	Dir string
	// Dist is the glob of built distributions to upload. Defaults to
	// "dist/*".
	Dist string
	// Repository is the twine repository name (`--repository`), e.g.
	// "testpypi" or "pypi". Empty uses twine's default (pypi).
	Repository string
	// Tool selects the uploader: "twine" (default) or "uv".
	Tool string
	// TokenSecret is the sparkwing secret name holding the registry token.
	// For twine it is exported as TWINE_PASSWORD (with TWINE_USERNAME set
	// to "__token__"); for uv as UV_PUBLISH_TOKEN.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// PyPIPublish uploads built distributions to a Python package index with
// `twine upload` (default) or `uv publish`. Honors the dry-run
// convention: under DryRun or SPARKWING_DRY_RUN it echoes the argv and
// returns nil without reaching the index.
func PyPIPublish(ctx context.Context, cfg PyPIPublishConfig) error {
	tool := cfg.Tool
	if tool == "" {
		tool = "twine"
	}
	if tool != "twine" && tool != "uv" {
		return fmt.Errorf("release: PyPIPublish Tool %q must be \"twine\" or \"uv\"", tool)
	}
	name, args := pypiArgs(tool, cfg)
	return step.Run(ctx, "pypi publish", func(ctx context.Context) error {
		if cfg.DryRun || dryRun() {
			echoArgv(ctx, name, args)
			return nil
		}
		cmd := sparkwing.Exec(ctx, name, args...)
		if cfg.Dir != "" {
			cmd = cmd.Dir(cfg.Dir)
		}
		if cfg.TokenSecret != "" {
			token, err := sparkwing.Secret(ctx, cfg.TokenSecret)
			if err != nil {
				return err
			}
			if tool == "twine" {
				cmd = cmd.Env("TWINE_USERNAME", "__token__").Env("TWINE_PASSWORD", token)
			} else {
				cmd = cmd.Env("UV_PUBLISH_TOKEN", token)
			}
		}
		_, err := cmd.Run()
		return err
	})
}

// pypiArgs builds the uploader command and argv for the selected tool.
// Pure, for testing.
func pypiArgs(tool string, cfg PyPIPublishConfig) (name string, args []string) {
	dist := cfg.Dist
	if dist == "" {
		dist = "dist/*"
	}
	if tool == "uv" {
		args = []string{"publish"}
		if cfg.Repository != "" {
			args = append(args, "--publish-url", cfg.Repository)
		}
		return "uv", append(args, dist)
	}
	args = []string{"upload"}
	if cfg.Repository != "" {
		args = append(args, "--repository", cfg.Repository)
	}
	return "twine", append(args, dist)
}
