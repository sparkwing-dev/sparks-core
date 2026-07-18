package release

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// CrossBuildConfig describes a GOOS/GOARCH build matrix for a Go binary.
type CrossBuildConfig struct {
	// MainPkg is the package to build, relative to the repo root (e.g.
	// "./cmd/app"). Defaults to ".".
	MainPkg string
	// BinaryName is the base output name; each artifact is
	// "<BinaryName>_<Version>_<goos>_<goarch>" (with a ".exe" suffix on
	// windows). Required.
	BinaryName string
	// Version stamps the artifact filenames. Required.
	Version string
	// Platforms is the list of "goos/arch" pairs to build. Empty defaults
	// to linux/amd64, linux/arm64, and darwin/arm64.
	Platforms []string
	// OutDir is where artifacts are written, relative to the repo root.
	// Defaults to "dist".
	OutDir string
	// LDFlags, when set, is passed through as `-ldflags`.
	LDFlags string
}

// CrossBuildGo compiles the configured GOOS/GOARCH matrix and returns the
// artifact paths (relative to the repo root), sorted for determinism.
// This is a local build that mutates nothing remote, so it always runs
// for real, including under SPARKWING_DRY_RUN.
func CrossBuildGo(ctx context.Context, cfg CrossBuildConfig) ([]string, error) {
	if cfg.BinaryName == "" {
		return nil, fmt.Errorf("release: CrossBuildGo BinaryName is required")
	}
	if cfg.Version == "" {
		return nil, fmt.Errorf("release: CrossBuildGo Version is required")
	}
	if cfg.MainPkg == "" {
		cfg.MainPkg = "."
	}
	if cfg.OutDir == "" {
		cfg.OutDir = "dist"
	}
	platforms := cfg.Platforms
	if len(platforms) == 0 {
		platforms = []string{"linux/amd64", "linux/arm64", "darwin/arm64"}
	}

	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}
	if err := os.MkdirAll(filepath.Join(root, cfg.OutDir), 0o755); err != nil {
		return nil, fmt.Errorf("release: mkdir %s: %w", cfg.OutDir, err)
	}

	var artifacts []string
	err := step.Run(ctx, "cross-build ("+cfg.Version+")", func(ctx context.Context) error {
		for _, platform := range platforms {
			goos, goarch, perr := splitPlatform(platform)
			if perr != nil {
				return perr
			}
			out := ArtifactPath(cfg.OutDir, cfg.BinaryName, cfg.Version, goos, goarch)
			args := buildArgs(cfg, out)
			sparkwing.Info(ctx, "building %s/%s -> %s", goos, goarch, out)
			if _, err := sparkwing.Exec(ctx, "go", args...).
				Env("GOOS", goos).
				Env("GOARCH", goarch).
				Env("CGO_ENABLED", "0").
				Run(); err != nil {
				return fmt.Errorf("release: build %s: %w", platform, err)
			}
			artifacts = append(artifacts, out)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(artifacts)
	return artifacts, nil
}

// buildArgs assembles the `go build` argv (without the GOOS/GOARCH env,
// which the caller sets). Pure, for testing.
func buildArgs(cfg CrossBuildConfig, out string) []string {
	args := []string{"build", "-o", out}
	if cfg.LDFlags != "" {
		args = append(args, "-ldflags", cfg.LDFlags)
	}
	return append(args, cfg.MainPkg)
}

// ArtifactPath returns the output path for one matrix entry:
// "<outDir>/<binary>_<version>_<goos>_<goarch>", with a ".exe" suffix on
// windows.
func ArtifactPath(outDir, binary, version, goos, goarch string) string {
	name := fmt.Sprintf("%s_%s_%s_%s", binary, version, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(outDir, name)
}

// splitPlatform parses a "goos/arch" pair.
func splitPlatform(platform string) (goos, goarch string, err error) {
	parts := strings.Split(strings.TrimSpace(platform), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("release: invalid platform %q (want goos/arch)", platform)
	}
	return parts[0], parts[1], nil
}

// ChecksumConfig describes a sha256 manifest over release artifacts.
type ChecksumConfig struct {
	// Dir is the directory whose files are checksummed, relative to the
	// repo root. Defaults to "dist". Used only when Files is empty.
	Dir string
	// Files is an explicit list of files to checksum (relative to the repo
	// root). When empty, every regular file directly in Dir is used,
	// excluding the output file itself.
	Files []string
	// Output is the checksums file to write, relative to the repo root.
	// Defaults to "<Dir>/checksums.txt".
	Output string
}

// Checksums writes a sha256sum-format manifest (`<hex>  <basename>` per
// line) over the configured artifacts. Local only; always runs for real.
func Checksums(ctx context.Context, cfg ChecksumConfig) error {
	if cfg.Dir == "" {
		cfg.Dir = "dist"
	}
	if cfg.Output == "" {
		cfg.Output = filepath.Join(cfg.Dir, "checksums.txt")
	}
	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}

	files := cfg.Files
	if len(files) == 0 {
		listed, err := listDirFiles(filepath.Join(root, cfg.Dir), filepath.Base(cfg.Output))
		if err != nil {
			return err
		}
		for _, name := range listed {
			files = append(files, filepath.Join(cfg.Dir, name))
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("release: no files to checksum in %s", cfg.Dir)
	}

	return step.Run(ctx, "checksums", func(ctx context.Context) error {
		var b strings.Builder
		for _, rel := range files {
			sum, err := sha256File(filepath.Join(root, rel))
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "%s  %s\n", sum, filepath.Base(rel))
		}
		outPath := filepath.Join(root, cfg.Output)
		if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
			return fmt.Errorf("release: write %s: %w", cfg.Output, err)
		}
		sparkwing.Info(ctx, "wrote %d checksums to %s", len(files), cfg.Output)
		return nil
	})
}

// listDirFiles returns the regular-file names directly in dir, excluding
// the manifest file itself, sorted for a deterministic manifest.
func listDirFiles(dir, exclude string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("release: read dir %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == exclude {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// sha256File returns the lowercase hex SHA-256 of a file's contents.
func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("release: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
