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
	// ArgoCD with an empty Server probes the in-cluster service.
	ArgoCD gitops.ArgoCDConfig
}

// Run restarts deployments via kubectl, or pushes image tags to the gitops
// repo and kicks ArgoCD. The routing is cfg.Local, not whether the code runs
// inside a cluster, so a laptop deploy to prod still goes through gitops.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Local {
		sparkwing.Info(ctx, "deploy: local -> kubectl rollout restart (ns=%s)", cfg.Namespace)
		return kube.DeployKubectl(ctx, cfg.Images, cfg.DeployMap, cfg.Namespace)
	}

	sparkwing.Info(ctx, "deploy: remote -> gitops + argocd (app=%s)", cfg.AppName)
	deployment := gitops.DeployConfig{
		GitopsRepo:  cfg.GitopsRepo,
		GitopsPath:  cfg.GitopsPath,
		ECR:         cfg.ECR,
		Images:      cfg.Images,
		Tag:         cfg.Tag,
		FilePatches: cfg.FilePatches,
	}
	_, err := gitops.Deploy(ctx, deployment)
	if err != nil {
		return err
	}
	return gitops.SyncDeployment(ctx, cfg.ArgoCD, cfg.AppName, deployment)
}
