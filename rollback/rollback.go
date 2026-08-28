// Package rollback reverts the most recent deployment, routing the way the
// deploy package does: `kubectl rollout undo` locally, or a gitops revert
// plus ArgoCD sync remotely.
package rollback

import (
	"context"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gitops"
	"github.com/sparkwing-dev/sparks-core/kube"
)

// Config configures a rollback. Populate both the kube fields (local path)
// and the gitops fields (remote path) so one config works wherever it runs.
type Config struct {
	Deployments []string
	Namespace   string
	// Context empty resolves via kube.ResolveContext, which fails closed.
	Context    string
	GitopsRepo string
	// GitopsCommit defaults to "HEAD", the most recent deploy.
	GitopsCommit string
	// AppName is the ArgoCD application to sync after a remote revert.
	AppName string
	// ArgoCD with an empty Server probes the in-cluster service.
	ArgoCD gitops.ArgoCDConfig
	// Local routes to the kubectl path instead of gitops.
	Local bool
}

// Run rolls back the most recent deployment, mirroring deploy.Run's routing.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Local {
		sparkwing.Info(ctx, "rollback: local -> kubectl rollout undo (ns=%s)", cfg.Namespace)
		return kube.RolloutUndo(ctx, kube.RolloutUndoConfig{
			Deployments: cfg.Deployments,
			Namespace:   cfg.Namespace,
			Context:     cfg.Context,
		})
	}

	sparkwing.Info(ctx, "rollback: remote -> gitops revert + argocd (app=%s)", cfg.AppName)
	changed, err := gitops.Revert(ctx, gitops.RevertConfig{
		GitopsRepo: cfg.GitopsRepo,
		Commit:     cfg.GitopsCommit,
	})
	if err != nil {
		return err
	}
	if changed && cfg.AppName != "" {
		return gitops.SyncArgoCD(ctx, cfg.ArgoCD, cfg.AppName)
	}
	sparkwing.Info(ctx, "rollback: nothing reverted - skipping argocd sync")
	return nil
}
