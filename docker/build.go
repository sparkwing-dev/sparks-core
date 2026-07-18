package docker

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
	sparkwingDocker "github.com/sparkwing-dev/sparkwing/sparkwing/docker"

	"github.com/sparkwing-dev/sparks-core/step"
)

// BuildConfig configures a Docker image build and push.
type BuildConfig struct {
	Image      string
	Dockerfile string
	Context    string
	Registries []string
	Tags       sparkwingDocker.ImageTag
	AWSProfile string
	Platform   string
	// BuildArgs are forwarded as --build-arg K=V (order-independent). Set
	// PROXY_URL here to route package installs through a dependency proxy.
	BuildArgs map[string]string
	// CacheFrom are BuildKit --cache-from specs; see BuildCacheRef.
	CacheFrom []string
	// CacheTo are BuildKit --cache-to specs; see BuildCacheRef.
	CacheTo []string
}

// ecrLoginOnce ensures each ECR registry is authenticated exactly
// once, even when BuildAndPush is called concurrently from multiple
// goroutines.
var (
	ecrLoginMu    sync.Mutex
	ecrLoginOnces = map[string]*sync.Once{}
	ecrLoginErrs  = map[string]error{}
)

func ensureECRLogin(ctx context.Context, registry, awsProfile string) error {
	ecrLoginMu.Lock()
	once, exists := ecrLoginOnces[registry]
	if !exists {
		once = &sync.Once{}
		ecrLoginOnces[registry] = once
	}
	ecrLoginMu.Unlock()

	once.Do(func() {
		ecrLoginErrs[registry] = ECRLogin(ctx, registry, awsProfile)
	})
	return ecrLoginErrs[registry]
}

// BuildAndPush builds a Docker image and pushes to all registries.
// Each image is built with multiple tags locally; only the deploy-
// relevant tag is actually pushed per registry to keep push time
// bounded. Safe to call concurrently -- ECR login is serialized via
// sync.Once.
//
// When SPARKWING_KIND_CLUSTER is set, the push path is replaced by
// `kind load docker-image`: the image is injected into the kind
// cluster's containerd directly, bypassing the registry round-trip.
func BuildAndPush(ctx context.Context, cfg BuildConfig) error {
	if cfg.Context == "" {
		cfg.Context = "."
	}

	for _, reg := range cfg.Registries {
		if IsECR(reg) {
			if err := ensureECRLogin(ctx, reg, cfg.AWSProfile); err != nil {
				return err
			}
		}
	}

	pushTags := make([]string, 0, len(cfg.Registries))
	buildTags := []string{
		cfg.Image + ":" + cfg.Tags.DeployTag(),
	}
	for _, reg := range cfg.Registries {
		var primary string
		if IsECR(reg) {
			primary = fmt.Sprintf("%s/%s:%s", reg, cfg.Image, cfg.Tags.ProdTag())
		} else {
			primary = fmt.Sprintf("%s/%s:%s", reg, cfg.Image, cfg.Tags.DeployTag())
		}
		pushTags = append(pushTags, primary)
		buildTags = append(buildTags, primary)
	}

	if err := step.Run(ctx, "build ("+cfg.Image+")", func(ctx context.Context) error {
		args := []string{"build", "-f", cfg.Dockerfile}
		if cfg.Platform != "" {
			args = append(args, "--platform", cfg.Platform)
		}
		args = append(args, buildArgFlags(cfg.BuildArgs)...)
		for _, c := range cfg.CacheFrom {
			args = append(args, "--cache-from", c)
		}
		for _, c := range cfg.CacheTo {
			args = append(args, "--cache-to", c)
		}
		for _, t := range buildTags {
			args = append(args, "-t", t)
		}
		args = append(args, cfg.Context)
		if dryRun() {
			echoArgv(ctx, "docker", args)
			return nil
		}
		_, err := sparkwing.Exec(ctx, "docker", args...).Env(buildKitEnv, "1").Run()
		return err
	}); err != nil {
		return err
	}

	if kindCluster := os.Getenv("SPARKWING_KIND_CLUSTER"); kindCluster != "" {
		loadTag := cfg.Image + ":" + cfg.Tags.DeployTag()
		loadArgs := []string{"load", "docker-image", loadTag, "--name", kindCluster}
		return step.Run(ctx, "kind load ("+cfg.Image+")", func(ctx context.Context) error {
			if dryRun() {
				echoArgv(ctx, "kind", loadArgs)
				return nil
			}
			sparkwing.Info(ctx, "kind load %s -> %s", loadTag, kindCluster)
			return step.Exec(ctx, "kind", loadArgs...)
		})
	}

	for _, t := range pushTags {
		pushTag := t
		if err := step.Run(ctx, "push ("+cfg.Image+")", func(ctx context.Context) error {
			if dryRun() {
				echoArgv(ctx, "docker", []string{"push", pushTag})
				return nil
			}
			sparkwing.Info(ctx, "pushing %s", pushTag)
			if err := step.Exec(ctx, "docker", "push", pushTag); err != nil {
				return err
			}
			sparkwing.Info(ctx, "pushed %s", pushTag)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
