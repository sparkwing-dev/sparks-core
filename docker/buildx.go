package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// defaultPlatforms is the buildx target matrix used when BuildxConfig
// leaves Platforms empty.
const defaultPlatforms = "linux/amd64,linux/arm64"

// defaultLocalCacheDir is the host directory BuildCacheRef points a local
// BuildKit cache at when no ref is given.
const defaultLocalCacheDir = ".buildx-cache"

// buildKitEnv forces BuildKit for a `docker build`/`docker buildx` call so
// --cache-from/--cache-to type specs and multi-arch manifests are honored.
const buildKitEnv = "DOCKER_BUILDKIT"

// buildArgFlags renders a build-arg map as a stable, sorted
// --build-arg K=V argv slice (sorted so the output is deterministic and
// testable).
func buildArgFlags(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(m)*2)
	for _, k := range keys {
		args = append(args, "--build-arg", k+"="+m[k])
	}
	return args
}

// BuildxConfig drives BuildxPublish.
type BuildxConfig struct {
	// Image is the repository path within the registry (e.g. "myapp" or
	// "team/myapp"). Required.
	Image string
	// Registry is the registry host/prefix to push to. Required.
	Registry string
	// Tags are the tags to publish; each becomes registry/image:tag.
	// Empty defaults to a single "latest" tag.
	Tags []string
	// Platforms is the comma-separated buildx target matrix. Empty
	// defaults to defaultPlatforms.
	Platforms string
	// Dockerfile is the Dockerfile path. Empty defaults to "Dockerfile".
	Dockerfile string
	// Context is the build context directory. Empty defaults to ".".
	Context string
	// BuildArgs are forwarded as --build-arg K=V (order-independent).
	BuildArgs map[string]string
	// CacheFrom are BuildKit --cache-from specs (see BuildCacheRef).
	CacheFrom []string
	// CacheTo are BuildKit --cache-to specs (see BuildCacheRef).
	CacheTo []string
	// DryRun echoes the docker argv without executing, same as setting
	// SPARKWING_DRY_RUN. Either signal activates dry-run.
	DryRun bool
}

// imageRefs expands a registry/image into the full push references, one
// per tag.
func imageRefs(registry, image string, tags []string) []string {
	refs := make([]string, 0, len(tags))
	for _, t := range tags {
		refs = append(refs, fmt.Sprintf("%s/%s:%s", registry, image, t))
	}
	return refs
}

// buildxArgs builds the `docker buildx build ... --push` argv for a
// multi-arch publish. Pure, for testing.
func buildxArgs(cfg BuildxConfig, refs []string) []string {
	args := []string{"buildx", "build", "--platform", cfg.Platforms, "-f", cfg.Dockerfile}
	for _, r := range refs {
		args = append(args, "-t", r)
	}
	args = append(args, buildArgFlags(cfg.BuildArgs)...)
	for _, c := range cfg.CacheFrom {
		args = append(args, "--cache-from", c)
	}
	for _, c := range cfg.CacheTo {
		args = append(args, "--cache-to", c)
	}
	args = append(args, "--push", cfg.Context)
	return args
}

// BuildxPublish builds a multi-arch image with `docker buildx build` and
// pushes a single manifest to the registry (--push). Authenticate first
// with RegistryLogin; BuildxPublish only builds and pushes.
//
// Under dry-run (BuildxConfig.DryRun or SPARKWING_DRY_RUN) it echoes the
// docker argv and returns without invoking docker, so the publish
// pipeline renders and goes green without a builder, QEMU, or registry.
func BuildxPublish(ctx context.Context, cfg BuildxConfig) error {
	if cfg.Image == "" || cfg.Registry == "" {
		return fmt.Errorf("docker.BuildxPublish: Image and Registry are required")
	}
	if cfg.Platforms == "" {
		cfg.Platforms = defaultPlatforms
	}
	if cfg.Dockerfile == "" {
		cfg.Dockerfile = "Dockerfile"
	}
	if cfg.Context == "" {
		cfg.Context = "."
	}
	if len(cfg.Tags) == 0 {
		cfg.Tags = []string{"latest"}
	}
	args := buildxArgs(cfg, imageRefs(cfg.Registry, cfg.Image, cfg.Tags))
	return step.Run(ctx, "buildx publish ("+cfg.Image+")", func(ctx context.Context) error {
		if cfg.DryRun || dryRun() {
			echoArgv(ctx, "docker", args)
			return nil
		}
		sparkwing.Info(ctx, "building and pushing %s (%s)", cfg.Image, cfg.Platforms)
		_, err := sparkwing.Exec(ctx, "docker", args...).Env(buildKitEnv, "1").Run()
		return err
	})
}

// BuildCacheRef resolves a cache backend and ref into the BuildKit
// --cache-from and --cache-to spec strings for BuildConfig.CacheFrom /
// BuildConfig.CacheTo (or BuildxConfig).
//
// backend "local" (or empty) caches to a host directory (ref, defaulting
// to defaultLocalCacheDir): runs green with only Docker. backend "ecr" or
// "gar" caches to a registry ref (required), shared across runners; the
// two differ only in auth, handled separately by RegistryLogin. The
// cacheTo spec adds mode=max so all layers are exported, not just the
// final stage.
func BuildCacheRef(backend, ref string) (cacheFrom, cacheTo string, err error) {
	switch backend {
	case "local", "":
		dir := ref
		if dir == "" {
			dir = defaultLocalCacheDir
		}
		return "type=local,src=" + dir, "type=local,dest=" + dir + ",mode=max", nil
	case "ecr", "gar":
		if ref == "" {
			return "", "", fmt.Errorf("docker.BuildCacheRef: %s backend requires a cache ref", backend)
		}
		return "type=registry,ref=" + ref, "type=registry,ref=" + ref + ",mode=max", nil
	default:
		return "", "", fmt.Errorf("docker.BuildCacheRef: unknown backend %q (want local, ecr, or gar)", backend)
	}
}
