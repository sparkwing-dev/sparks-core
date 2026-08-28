// Package pipelines chains the build, push, and deploy building blocks from
// sparks-core's other packages into single consumer-facing pipeline types.
package pipelines

import (
	"context"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	sparkwingDocker "github.com/sparkwing-dev/sparkwing/sparkwing/docker"

	"github.com/sparkwing-dev/sparks-core/deploy"
	"github.com/sparkwing-dev/sparks-core/docker"
	"github.com/sparkwing-dev/sparks-core/gitops"
)

// DockerDeploy is a one-node pipeline that builds a Docker image, pushes it
// to the configured registries, and deploys via gitops or kubectl.
type DockerDeploy struct {
	sparkwing.Base

	Image string
	// Dockerfile defaults to "Dockerfile".
	Dockerfile string
	// Context defaults to ".".
	Context string
	// ECR is the registry URL used for prod pushes and gitops image matching.
	ECR string
	// Registry redirects the push elsewhere without changing the ECR
	// endpoint gitops matches images on.
	Registry string
	// GitopsRepo is the SSH URL for the gitops repo.
	GitopsRepo string
	GitopsPath string
	// AppName is the ArgoCD application name.
	AppName string
	// ArgoCD with an empty Server probes the in-cluster service.
	ArgoCD gitops.ArgoCDConfig
	// Namespace is also the kubectl -n target for local deploys.
	Namespace string
	// DeployMap maps image name to k8s deployment, defaulting to
	// "deploy/<image>".
	DeployMap map[string]string
	// TestCmd empty skips the test step.
	TestCmd string
	// Platform empty uses the host default.
	Platform string
	// SkipTests bypasses TestCmd even when it is set, for pipelines that fan
	// tests out to an earlier node.
	SkipTests bool
}

// Plan returns the one-node DAG that runs build, push, and deploy as a
// single step.
func (d *DockerDeploy) Plan(_ context.Context, plan *sparkwing.Plan, _ sparkwing.NoInputs, run sparkwing.RunContext) error {
	sparkwing.Job(plan, run.Pipeline, d.Run)
	return nil
}

// Run executes the full build, push, and deploy sequence as one step.
func (d *DockerDeploy) Run(ctx context.Context) error {
	d.applyDefaults()

	registries, err := d.resolveRegistries()
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
		ArgoCD:     d.ArgoCD,
	})
}

func (d *DockerDeploy) applyDefaults() {
	if d.Dockerfile == "" {
		d.Dockerfile = "Dockerfile"
	}
	if d.Context == "" {
		d.Context = "."
	}
	if d.DeployMap == nil {
		d.DeployMap = map[string]string{d.Image: "deploy/" + d.Image}
	}
}

func (d *DockerDeploy) resolveRegistries() ([]string, error) {
	return docker.Registries(d.Registry, d.ECR)
}
