package docker

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
	sparkwingDocker "github.com/sparkwing-dev/sparkwing/sparkwing/docker"

	"github.com/sparkwing-dev/sparks-core/step"
)

type BuildConfig struct {
	Image      string
	Dockerfile string
	Context    string
	Registries []string
	Tags       sparkwingDocker.ImageTag
	AWSProfile string
	Platform   string
	BuildArgs  map[string]string
	// CacheFrom and CacheTo are BuildKit specs; see BuildCacheRef.
	CacheFrom []string
	CacheTo   []string
}

// Each ECR registry authenticates exactly once, even under concurrent
// BuildAndPush calls.
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

// BuildAndPush builds a Docker image and pushes it to every registry. It
// tags the image several ways locally but pushes only the deploy-relevant
// tag per registry, keeping push time bounded. It is safe to call
// concurrently.
func BuildAndPush(ctx context.Context, cfg BuildConfig) error {
	_, err := BuildAndPushResult(ctx, cfg)
	return err
}

// BuildAndPushResult retains published manifest digests so mutable tags cannot select different content.
func BuildAndPushResult(ctx context.Context, cfg BuildConfig) (sparkwingDocker.BuildResult, error) {
	result := sparkwingDocker.BuildResult{Digests: map[string]string{}}

	if cfg.Context == "" {
		cfg.Context = "."
	}

	for _, reg := range cfg.Registries {
		if IsECR(reg) {
			if err := ensureECRLogin(ctx, reg, cfg.AWSProfile); err != nil {
				return result, err
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
		return result, err
	}

	for _, t := range pushTags {
		pushTag := t
		if err := step.Run(ctx, "push ("+cfg.Image+")", func(ctx context.Context) error {
			if dryRun() {
				echoArgv(ctx, "docker", []string{"push", pushTag})
				return nil
			}
			sparkwing.Info(ctx, "pushing %s", pushTag)
			output, err := sparkwing.Exec(ctx, "docker", "push", pushTag).Run()
			if err != nil {
				return err
			}
			tag := pushTag[strings.LastIndex(pushTag, ":")+1:]
			if digest := pushedManifestDigest(output.Stdout+"\n"+output.Stderr, tag); digest != "" {
				result.Digests[pushTag] = digest
			}
			sparkwing.Info(ctx, "pushed %s", pushTag)
			return nil
		}); err != nil {
			return result, err
		}
	}
	result.Image = buildTags[0]
	if len(pushTags) > 0 {
		result.Image = pushTags[0]
	}
	result.Registries = append(result.Registries, cfg.Registries...)
	return result, nil
}

func pushedManifestDigest(output, tag string) string {
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(tag) + `: digest: (sha256:[a-f0-9]{64}) size: [0-9]+\s*$`)
	digest := ""
	for _, match := range pattern.FindAllStringSubmatch(output, -1) {
		if digest != "" && digest != match[1] {
			return ""
		}
		digest = match[1]
	}
	return digest
}
