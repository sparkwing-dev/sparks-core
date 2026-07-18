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
	// Required: at least one tag. Prefer an immutable, content-addressed
	// tag over a floating one like "latest".
	Tags []string
	// Platforms is the comma-separated buildx target matrix. Empty
	// defaults to defaultPlatforms.
	Platforms string
	// Builder is the buildx builder instance to target (--builder). Empty
	// uses the current default builder; a multi-platform --push requires a
	// docker-container (or other non-docker) driver, so set this to a
	// builder created with `docker buildx create --driver docker-container`.
	Builder string
	// Dockerfile is the Dockerfile path. Empty defaults to "Dockerfile".
	Dockerfile string
	// Target is the Dockerfile stage to build (--target). Empty builds the
	// final stage.
	Target string
	// Context is the build context directory. Empty defaults to ".".
	Context string
	// BuildArgs are forwarded as --build-arg K=V (order-independent).
	BuildArgs map[string]string
	// CacheFrom are BuildKit --cache-from specs (see BuildCacheRef).
	CacheFrom []string
	// CacheTo are BuildKit --cache-to specs (see BuildCacheRef).
	CacheTo []string
	// ExtraArgs are spliced into the buildx argv immediately before
	// --push, an escape hatch for flags without a dedicated field
	// (--secret, --ssh, --provenance, --sbom, --no-cache, --pull, ...).
	ExtraArgs []string
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
	args := []string{"buildx", "build"}
	if cfg.Builder != "" {
		args = append(args, "--builder", cfg.Builder)
	}
	args = append(args, "--platform", cfg.Platforms, "-f", cfg.Dockerfile)
	if cfg.Target != "" {
		args = append(args, "--target", cfg.Target)
	}
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
	args = append(args, cfg.ExtraArgs...)
	args = append(args, "--push", cfg.Context)
	return args
}

// BuildxPublish builds a multi-arch image with `docker buildx build` and
// pushes a single manifest to the registry (--push). Authenticate first
// with RegistryLogin; BuildxPublish only builds and pushes.
//
// Prerequisites for a real run: a buildx builder backed by a
// docker-container (or other non-docker) driver, because the default
// docker-driver builder rejects a multi-platform --push. Create one with
// `docker buildx create --driver docker-container --use` and register
// QEMU/binfmt for cross-arch emulation, or point BuildxConfig.Builder at a
// preconfigured builder. Under SPARKWING_DRY_RUN the docker argv is echoed
// and docker is never invoked, so the pipeline renders and goes green
// without a builder, QEMU, or registry.
func BuildxPublish(ctx context.Context, cfg BuildxConfig) error {
	if cfg.Image == "" || cfg.Registry == "" || len(cfg.Tags) == 0 {
		return fmt.Errorf("docker.BuildxPublish: Image, Registry, and at least one Tag are required")
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
	args := buildxArgs(cfg, imageRefs(cfg.Registry, cfg.Image, cfg.Tags))
	return step.Run(ctx, "buildx publish ("+cfg.Image+")", func(ctx context.Context) error {
		if dryRun() {
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
// to defaultLocalCacheDir): runs green with only Docker. When the local
// dir sits under the build context (the default defaultLocalCacheDir does,
// alongside a "." context), add it to .dockerignore so a `COPY . .` does
// not sweep the cache into the image. Any registry backend ("registry",
// "ecr", "gar", "ghcr") caches to a registry ref (required), shared across
// runners; they emit the same spec and differ only in the auth
// RegistryLogin performs separately. The cacheTo spec adds mode=max so all
// layers are exported, not just the final stage.
func BuildCacheRef(backend, ref string) (cacheFrom, cacheTo string, err error) {
	switch backend {
	case "local", "":
		dir := ref
		if dir == "" {
			dir = defaultLocalCacheDir
		}
		return "type=local,src=" + dir, "type=local,dest=" + dir + ",mode=max", nil
	case "registry", "ecr", "gar", "ghcr":
		if ref == "" {
			return "", "", fmt.Errorf("docker.BuildCacheRef: %s backend requires a cache ref", backend)
		}
		return "type=registry,ref=" + ref, "type=registry,ref=" + ref + ",mode=max", nil
	default:
		return "", "", fmt.Errorf("docker.BuildCacheRef: unknown backend %q (want local, registry, ecr, gar, or ghcr)", backend)
	}
}
