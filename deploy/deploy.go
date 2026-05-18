// Package deploy is sparks-core's deploy dispatcher: pick between
// kind/kustomize, kubectl rollout restart, and gitops+ArgoCD based
// on environment hints and the caller's declared target.
package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"

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
}

// Run executes a deployment using the appropriate strategy based on
// the target:
//
//   - Local (kind): restarts deployments directly via kubectl, or
//     applies the repo-owned k8s/ kustomization when present.
//   - Remote (prod): pushes image tags to gitops repo and kicks
//     ArgoCD.
//
// The routing decision is based on cfg.Local and the SPARKWING_KIND_CLUSTER
// env var set by sparkwing, not on whether the code is running inside a
// cluster. Laptop deploys to prod go through gitops.
func Run(ctx context.Context, cfg Config) error {
	// SPARKWING_KIND_CLUSTER flips deploys into local-kubectl mode
	// without requiring each consumer pipeline to set cfg.Local
	// manually. sparkwing sets this env var when --on resolves to a kind
	// profile, so any pipeline running against a kind cluster lands
	// here even if the pipeline was written for a prod-only gitops
	// flow.
	if !cfg.Local && os.Getenv("SPARKWING_KIND_CLUSTER") != "" {
		kindCluster := os.Getenv("SPARKWING_KIND_CLUSTER")
		// Prefer the repo-owned kind manifests at $WORKDIR/k8s/ if
		// present. Those manifests reference short image names and a
		// kustomization.yaml whose image transformer we patch with
		// the current tag. Fall back to rollout restart for repos
		// that haven't added a k8s/ dir yet.
		kustDir := filepath.Join(sparkwing.WorkDir(), "k8s")
		if _, err := os.Stat(filepath.Join(kustDir, "kustomization.yaml")); err == nil {
			sparkwing.Info(ctx, "deploy: kind (%s) -> kustomize apply (%s)", kindCluster, kustDir)
			// For kind we use DeployTag (no -prod suffix); consumer
			// pipelines typically pass ProdTag, so strip here.
			tag := strings.TrimSuffix(cfg.Tag, "-prod")
			return kube.DeployKindKustomize(ctx, kube.KindKustomizeConfig{
				Cluster:      kindCluster,
				KustomizeDir: kustDir,
				Images:       cfg.Images,
				Tag:          tag,
				DeployMap:    cfg.DeployMap,
				Namespace:    cfg.Namespace,
			})
		}
		sparkwing.Info(ctx, "deploy: kind (%s) -> kubectl rollout restart (no k8s/ dir)", kindCluster)
		return kube.DeployKubectl(ctx, cfg.Images, cfg.DeployMap, cfg.Namespace)
	}

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
		return gitops.SyncArgoCD(ctx, cfg.AppName, cfg.Tag)
	}
	sparkwing.Info(ctx, "deploy: skipping argocd sync - tags unchanged")
	return nil
}
