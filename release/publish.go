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

type GitHubReleaseConfig struct {
	// Tag is required.
	Tag string
	// Title defaults to Tag.
	Title string
	Notes string
	// NotesFile takes precedence over Notes.
	NotesFile string
	// Assets are uploaded with the release, relative to the repo root.
	Assets []string
	// Repo is an "owner/name" override.
	Repo       string
	Draft      bool
	Prerelease bool
	// TokenSecret names the sparkwing secret exported as GH_TOKEN. Empty
	// relies on gh's ambient auth.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// GitHubRelease cuts a GitHub Release with `gh release create`.
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

type NpmPublishConfig struct {
	// Dir is the package directory, defaulting to ".".
	Dir string
	// Registry, Access, and Tag map to the like-named npm flags; each empty
	// value omits its flag.
	Registry   string
	Access     string
	Tag        string
	Provenance bool
	// TokenSecret names the sparkwing secret holding the npm auth token. npm
	// reads auth from an .npmrc rather than the environment, so a temporary
	// one is written and passed with --userconfig.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// NpmPublish publishes a package with `npm publish`.
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
			defer func() { _ = os.Remove(npmrc) }()
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

// writeNpmAuthConfig exists because npm does not authenticate from
// NODE_AUTH_TOKEN on its own; it reads `_authToken` lines from an .npmrc.
// The caller removes the returned path.
func writeNpmAuthConfig(registry string) (string, error) {
	f, err := os.CreateTemp("", "sparkwing-npmrc-*")
	if err != nil {
		return "", fmt.Errorf("release: create npmrc: %w", err)
	}
	line := "//" + npmAuthHost(registry) + ":_authToken=${NODE_AUTH_TOKEN}\n"
	if _, werr := f.WriteString(line); werr != nil {
		f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("release: write npmrc: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("release: write npmrc: %w", cerr)
	}
	return f.Name(), nil
}

func npmAuthHost(registry string) string {
	if registry != "" {
		if u, err := url.Parse(registry); err == nil && u.Host != "" {
			return u.Host + strings.TrimSuffix(u.Path, "/") + "/"
		}
	}
	return "registry.npmjs.org/"
}

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

type PyPIPublishConfig struct {
	// Dir is the working directory, defaulting to ".".
	Dir string
	// Dist is the glob of built distributions, defaulting to "dist/*".
	Dist string
	// Repository is a twine repository name; uv wants a full endpoint URL in
	// PublishURL instead. Each is ignored by the other tool.
	Repository string
	PublishURL string
	// Tool is "twine" (default) or "uv".
	Tool string
	// TokenSecret names the sparkwing secret exported as TWINE_PASSWORD, with
	// TWINE_USERNAME "__token__", or as UV_PUBLISH_TOKEN.
	TokenSecret string
	// DryRun forces echo-and-skip regardless of SPARKWING_DRY_RUN.
	DryRun bool
}

// PyPIPublish uploads built distributions with `twine upload` or `uv publish`.
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
