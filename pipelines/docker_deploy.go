// Package sparks exposes opinionated pipeline types that chain the
// build / push / deploy building blocks from sparks-core's other
// packages into a single consumer-facing shape.
//
// Typical use:
//
//	func init() {
//	    sparkwing.Register("build-test-deploy", func() any {
//	        return &sparks.DockerDeploy{
//	            Image:      "myapp",
//	            Dockerfile: "Dockerfile",
//	            ECR:        "633280902600.dkr.ecr.us-west-2.amazonaws.com",
//	            GitopsRepo: "git@github.com:org/gitops.git",
//	            GitopsPath: "myapp",
//	            AppName:    "myapp",
//	            Namespace:  "myapp",
//	            TestCmd:    "go test ./...",
//	        }
//	    })
//	}
package pipelines

import (
	"context"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	sparkwingDocker "github.com/sparkwing-dev/sparkwing/sparkwing/docker"

	"github.com/sparkwing-dev/sparks-core/deploy"
	"github.com/sparkwing-dev/sparks-core/docker"
)

// DockerDeploy is a one-node pipeline that builds a Docker image,
// pushes to the configured registries, and deploys via gitops
// (remote) or kubectl / kind-kustomize (local). Each phase (test,
// build+push, deploy) logs a step banner so failures show up cleanly
// in the pipeline log.
//
// Register via sparkwing.Register with your preferred pipeline name;
// this struct holds every knob the old sparks.DockerDeploy helper
// accepted plus a SkipTests toggle consumers can flip from CLI args.
type DockerDeploy struct {
	sparkwing.Base

	// Image is the image name (e.g. "myapp").
	Image string
	// Dockerfile is the path to the Dockerfile. Defaults to "Dockerfile".
	Dockerfile string
	// Context is the build context. Defaults to ".".
	Context string
	// ECR is the AWS ECR registry URL used for prod pushes + gitops
	// image matching.
	ECR string
	// GitopsRepo is the SSH URL for the gitops repo.
	GitopsRepo string
	// GitopsPath is the path within the gitops repo (e.g.
	// "myorg/myapp").
	GitopsPath string
	// AppName is the ArgoCD application name.
	AppName string
	// Namespace is the K8s namespace. For local/kind deploys this is
	// also the kubectl -n target.
	Namespace string
	// DeployMap maps image name -> k8s deployment (e.g. "myapp" ->
	// "deploy/myapp"). Defaults to image -> "deploy/<image>".
	DeployMap map[string]string
	// Cluster is the kind cluster name used when deploying locally.
	// Defaults to "sparktest".
	Cluster string
	// TestCmd is an optional shell command run before build. When
	// empty the test step is skipped entirely.
	TestCmd string
	// Platform targets a specific Docker build platform (e.g.
	// "linux/arm64"). Empty uses the host default.
	Platform string
	// SkipTests bypasses TestCmd even if it is set. Useful when the
	// consumer pipeline fans out tests into a separate node that
	// runs earlier in the DAG.
	SkipTests bool
}

// Plan returns the one-node DAG that runs build/push/deploy as a
// single step. Consumers that want per-phase DAG nodes (parallel
// build + test, gated deploy, etc.) can implement Plan() on their
// outer struct and call into DockerDeploy.Run / its sub-helpers
// directly instead of embedding.
func (d *DockerDeploy) Plan(_ context.Context, plan *sparkwing.Plan, _ sparkwing.NoInputs, run sparkwing.RunContext) error {
	sparkwing.Job(plan, run.Pipeline, d.Run)
	return nil
}

// Run executes the full build/push/deploy sequence as one pipeline
// step. This matches the pre-rewrite sparks.DockerDeploy shape so
// consumers upgrading from v0.26 sparks-core don't have to reshape
// their DAG. Consumers that want per-phase DAG nodes can copy this
// body into their own Plan() implementation.
func (d *DockerDeploy) Run(ctx context.Context) error {
	d.applyDefaults()

	registries, err := d.resolveRegistries(ctx)
	if err != nil {
		return err
	}
	tags, err := sparkwingDocker.ComputeTags(ctx)
	if err != nil {
		return err
	}

	sparkwing.Info(ctx, "registries: %s", strings.Join(registries, ", "))
	sparkwing.Info(ctx, "tag:        %s", tags.DeployTag())

	if d.TestCmd != "" && !d.SkipTests {
		sparkwing.Info(ctx, "==> test")
		if _, err := sparkwing.Bash(ctx, d.TestCmd).Run(); err != nil {
			return err
		}
	} else if d.SkipTests {
		sparkwing.Info(ctx, "==> test (skipped via SkipTests)")
	}

	sparkwing.Info(ctx, "==> build+push %s", d.Image)
	if err := docker.BuildAndPush(ctx, docker.BuildConfig{
		Image:      d.Image,
		Dockerfile: d.Dockerfile,
		Context:    d.Context,
		Registries: registries,
		Tags:       tags,
		Platform:   d.Platform,
	}); err != nil {
		return err
	}

	sparkwing.Info(ctx, "==> deploy app=%s ns=%s", d.AppName, d.Namespace)
	return deploy.Run(ctx, deploy.Config{
		GitopsRepo: d.GitopsRepo,
		GitopsPath: d.GitopsPath,
		ECR:        d.ECR,
		Images:     []string{d.Image},
		Tag:        tags.ProdTag(),
		AppName:    d.AppName,
		Namespace:  d.Namespace,
		DeployMap:  d.DeployMap,
	})
}

func (d *DockerDeploy) applyDefaults() {
	if d.Dockerfile == "" {
		d.Dockerfile = "Dockerfile"
	}
	if d.Context == "" {
		d.Context = "."
	}
	if d.Cluster == "" {
		d.Cluster = "sparktest"
	}
	if d.DeployMap == nil {
		d.DeployMap = map[string]string{d.Image: "deploy/" + d.Image}
	}
}

// resolveRegistries honors SPARKWING_REGISTRY (runner pod override),
// falling back to the configured ECR plus any local-kind registry
// hint. Mirrors the selection logic the old sparks.DockerDeploy
// helper did inline.
func (d *DockerDeploy) resolveRegistries(ctx context.Context) ([]string, error) {
	if r := os.Getenv("SPARKWING_REGISTRY"); r != "" {
		return []string{r}, nil
	}
	registries := []string{d.ECR}
	local, _ := docker.TryDetectLocalRegistries(d.Cluster)
	if len(local) > 0 {
		registries = append(local, registries...)
	}
	return registries, nil
}
