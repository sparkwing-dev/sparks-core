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
	// Local tags: only the content-addressed DeployTag. The former
	// "image:latest" local alias is dropped (SDK-010: floating-tag
	// retirement sweep).
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
		// Remote build tag: primary (ProdTag/DeployTag) only; no :latest
		// alias -- SDK-005 stopped pushing floating tags to registries.
		buildTags = append(buildTags, primary)
	}

	if err := step.Run(ctx, "build ("+cfg.Image+")", func(ctx context.Context) error {
		args := []string{"build", "-f", cfg.Dockerfile}
		if cfg.Platform != "" {
			args = append(args, "--platform", cfg.Platform)
		}
		for _, t := range buildTags {
			args = append(args, "-t", t)
		}
		args = append(args, cfg.Context)
		return step.Exec(ctx, "docker", args...)
	}); err != nil {
		return err
	}

	if kindCluster := os.Getenv("SPARKWING_KIND_CLUSTER"); kindCluster != "" {
		loadTag := cfg.Image + ":" + cfg.Tags.DeployTag()
		return step.Run(ctx, "kind load ("+cfg.Image+")", func(ctx context.Context) error {
			sparkwing.Info(ctx, "kind load %s -> %s", loadTag, kindCluster)
			return step.Exec(ctx, "kind", "load", "docker-image", loadTag, "--name", kindCluster)
		})
	}

	for _, t := range pushTags {
		pushTag := t
		if err := step.Run(ctx, "push ("+cfg.Image+")", func(ctx context.Context) error {
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
