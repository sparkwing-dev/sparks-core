// Package deploy is sparks-core's deploy dispatcher: pick between
// kubectl rollout restart and gitops+ArgoCD from the target the caller
// declared.
package deploy

import (
	"context"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gitops"
	"github.com/sparkwing-dev/sparks-core/kube"
)

// Config configures a deploy operation.
type Config struct {
	GitopsRepo  string
	GitopsPath  string
	ECR         string
	Images      []string
	Tag         string
	AppName     string
	Namespace   string
	DeployMap   map[string]string
	Local       bool
	FilePatches map[string]map[string]string
	// ArgoCD names the server the remote path syncs against and the
	// token it authenticates with. An empty Server probes the
	// in-cluster service.
	ArgoCD gitops.ArgoCDConfig
}

// Run executes a deployment using the appropriate strategy based on
// the target:
//
//   - Local: restarts deployments directly via kubectl.
//   - Remote (prod): pushes image tags to gitops repo and kicks
//     ArgoCD.
//
// The routing decision is cfg.Local, not whether the code is running
// inside a cluster. Laptop deploys to prod go through gitops.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Local {
		sparkwing.Info(ctx, "deploy: local -> kubectl rollout restart (ns=%s)", cfg.Namespace)
		return kube.DeployKubectl(ctx, cfg.Images, cfg.DeployMap, cfg.Namespace)
	}

	sparkwing.Info(ctx, "deploy: remote -> gitops + argocd (app=%s)", cfg.AppName)
	changed, err := gitops.Deploy(ctx, gitops.DeployConfig{
		GitopsRepo:  cfg.GitopsRepo,
		GitopsPath:  cfg.GitopsPath,
		ECR:         cfg.ECR,
		Images:      cfg.Images,
		Tag:         cfg.Tag,
		FilePatches: cfg.FilePatches,
	})
	if err != nil {
		return err
	}
	if changed {
		return gitops.SyncArgoCD(ctx, cfg.ArgoCD, cfg.AppName, cfg.Tag)
	}
	sparkwing.Info(ctx, "deploy: skipping argocd sync - tags unchanged")
	return nil
}
