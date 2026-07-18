package release

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

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
	// TokenSecret is the sparkwing secret name holding the npm auth token.
	// npm does not read auth from an environment variable, so NpmPublish
	// writes a temporary userconfig (.npmrc) mapping the target registry
	// to the token and passes it via `--userconfig`; the token itself is
	// exported as NODE_AUTH_TOKEN and interpolated by that .npmrc.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// NpmPublish publishes a package with `npm publish`. When TokenSecret is
// set it materializes a temporary .npmrc authenticating the target
// registry (npm reads auth from an `_authToken` line, not from an
// environment variable) and points npm at it with `--userconfig`. Honors
// the dry-run convention: under DryRun or SPARKWING_DRY_RUN it echoes the
// argv and returns nil without reaching the registry.
func NpmPublish(ctx context.Context, cfg NpmPublishConfig) error {
	args := npmArgs(cfg)
	return step.Run(ctx, "npm publish", func(ctx context.Context) error {
		if cfg.DryRun || dryRun() {
			echoArgv(ctx, "npm", args)
			return nil
		}
		runArgs := args
		if cfg.TokenSecret != "" {
			npmrc, err := writeNpmAuthConfig(cfg.Registry)
			if err != nil {
				return err
			}
			defer os.Remove(npmrc)
			runArgs = append(append([]string(nil), args...), "--userconfig", npmrc)
		}
		cmd := sparkwing.Exec(ctx, "npm", runArgs...)
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

// writeNpmAuthConfig writes a temporary npm userconfig (.npmrc) that maps
// the target registry to an `_authToken` interpolated from the
// NODE_AUTH_TOKEN environment variable, returning its path. npm does not
// authenticate from NODE_AUTH_TOKEN on its own; it reads `_authToken`
// lines from an .npmrc, so a real publish needs this file even though the
// token value stays in the environment. The caller passes the path via
// `--userconfig` and removes it afterward.
func writeNpmAuthConfig(registry string) (string, error) {
	f, err := os.CreateTemp("", "sparkwing-npmrc-*")
	if err != nil {
		return "", fmt.Errorf("release: create npmrc: %w", err)
	}
	line := "//" + npmAuthHost(registry) + ":_authToken=${NODE_AUTH_TOKEN}\n"
	if _, werr := f.WriteString(line); werr != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("release: write npmrc: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("release: write npmrc: %w", cerr)
	}
	return f.Name(), nil
}

// npmAuthHost derives the `host[/path]/` prefix an npm `_authToken` line
// keys on from a registry URL, defaulting to the public registry when the
// URL is empty or unparseable.
func npmAuthHost(registry string) string {
	if registry != "" {
		if u, err := url.Parse(registry); err == nil && u.Host != "" {
			return u.Host + strings.TrimSuffix(u.Path, "/") + "/"
		}
	}
	return "registry.npmjs.org/"
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
	// "testpypi" or "pypi". Used only by the twine tool; empty uses
	// twine's default (pypi). For the uv tool set PublishURL instead, since
	// uv wants a full endpoint URL rather than a named repository.
	Repository string
	// PublishURL is the full upload endpoint URL for the uv tool
	// (`--publish-url`), e.g. "https://test.pypi.org/legacy/". Used only
	// when Tool is "uv"; ignored by twine. Empty uses uv's default index.
	PublishURL string
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
		if cfg.PublishURL != "" {
			args = append(args, "--publish-url", cfg.PublishURL)
		}
		return "uv", append(args, dist)
	}
	args = []string{"upload"}
	if cfg.Repository != "" {
		args = append(args, "--repository", cfg.Repository)
	}
	return "twine", append(args, dist)
}
