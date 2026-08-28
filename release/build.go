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

type CrossBuildConfig struct {
	// MainPkg defaults to ".".
	MainPkg string
	// BinaryName and Version are required; see ArtifactPath for the names
	// they produce.
	BinaryName string
	Version    string
	// Platforms are "goos/arch" pairs, defaulting to linux/amd64,
	// linux/arm64, and darwin/arm64.
	Platforms []string
	// OutDir defaults to "dist".
	OutDir  string
	LDFlags string
	// Trimpath strips local filesystem paths for reproducible builds.
	Trimpath bool
	// Tags are comma-joined into a single -tags value.
	Tags []string
	// BuildFlags are appended verbatim after the managed flags.
	BuildFlags []string
	// EnableCgo builds with CGO_ENABLED=1; the default 0 yields static,
	// cross-compilable binaries.
	EnableCgo bool
}

// CrossBuildGo compiles the configured matrix and returns the artifact
// paths, sorted for determinism.
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

	cgoEnabled := "0"
	if cfg.EnableCgo {
		cgoEnabled = "1"
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
				Env("CGO_ENABLED", cgoEnabled).
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

func buildArgs(cfg CrossBuildConfig, out string) []string {
	args := []string{"build", "-o", out}
	if cfg.Trimpath {
		args = append(args, "-trimpath")
	}
	if len(cfg.Tags) > 0 {
		args = append(args, "-tags", strings.Join(cfg.Tags, ","))
	}
	if cfg.LDFlags != "" {
		args = append(args, "-ldflags", cfg.LDFlags)
	}
	args = append(args, cfg.BuildFlags...)
	return append(args, cfg.MainPkg)
}

// ArtifactPath returns "<outDir>/<binary>_<version>_<goos>_<goarch>", with
// a ".exe" suffix on windows.
func ArtifactPath(outDir, binary, version, goos, goarch string) string {
	name := fmt.Sprintf("%s_%s_%s_%s", binary, version, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(outDir, name)
}

func splitPlatform(platform string) (goos, goarch string, err error) {
	parts := strings.Split(strings.TrimSpace(platform), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("release: invalid platform %q (want goos/arch)", platform)
	}
	return parts[0], parts[1], nil
}

type ChecksumConfig struct {
	// Dir defaults to "dist" and is used only when Files is empty.
	Dir string
	// Files empty checksums every regular file directly in Dir, excluding
	// the output file itself.
	Files []string
	// Output defaults to "<Dir>/checksums.txt".
	Output string
}

// Checksums writes a sha256sum-format manifest over the configured artifacts.
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

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("release: read %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
