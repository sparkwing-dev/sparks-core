package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

const defaultPlatforms = "linux/amd64,linux/arm64"

const defaultLocalCacheDir = ".buildx-cache"

// buildKitEnv forces BuildKit, without which --cache-from/--cache-to type
// specs and multi-arch manifests are not honored.
const buildKitEnv = "DOCKER_BUILDKIT"

// buildArgFlags sorts keys so the argv is deterministic.
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

type BuildxConfig struct {
	// Image, Registry, and at least one Tag are required. Each tag becomes
	// registry/image:tag; prefer an immutable tag over a floating one.
	Image    string
	Registry string
	Tags     []string
	// Platforms is a comma-separated buildx matrix, defaulting to
	// defaultPlatforms.
	Platforms string
	// Builder empty uses the default builder, which rejects a multi-platform
	// --push: point this at one created with --driver docker-container.
	Builder string
	// Dockerfile defaults to "Dockerfile".
	Dockerfile string
	// Target empty builds the final stage.
	Target string
	// Context defaults to ".".
	Context   string
	BuildArgs map[string]string
	// CacheFrom and CacheTo are BuildKit specs; see BuildCacheRef.
	CacheFrom []string
	CacheTo   []string
	// ExtraArgs are spliced in immediately before --push.
	ExtraArgs []string
}

func imageRefs(registry, image string, tags []string) []string {
	refs := make([]string, 0, len(tags))
	for _, t := range tags {
		refs = append(refs, fmt.Sprintf("%s/%s:%s", registry, image, t))
	}
	return refs
}

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

// BuildxPublish builds a multi-arch image and pushes one manifest.
// Authenticate first with RegistryLogin. A real run needs a builder on a
// docker-container driver plus QEMU/binfmt, since the default docker-driver
// builder rejects a multi-platform --push.
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

// BuildCacheRef resolves a backend ("local", or one of the registry kinds
// "registry"/"ecr"/"gar"/"ghcr", which need a ref) into BuildKit
// --cache-from and --cache-to specs. A local cache dir under the build
// context belongs in .dockerignore, or a `COPY . .` sweeps it into the
// image. cacheTo adds mode=max so all layers export, not just the final
// stage.
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
