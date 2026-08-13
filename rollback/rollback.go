// Package rollback reverts the most recent deployment. It is the
// recovery counterpart to the deploy package and routes the same way:
//
//   - local: `kubectl rollout undo` on the named deployments.
//   - remote (prod): revert the last gitops commit and let ArgoCD sync
//     the cluster back to the prior image tags.
//
// Run is shaped as func(ctx) error so it drops straight into a Job's
// OnFailure handler, firing when a post-deploy Verify (e.g. a probe)
// reports the new revision unhealthy:
//
//	sw.Job(plan, "deploy", deployApp).
//	    Verify(probe.HTTP(healthURL).Retry(30).Check).
//	    OnFailure("rollback", func(ctx context.Context, _ sparkwing.Failure) error {
//	        return rollback.Run(ctx, rollback.Config{
//	            Deployments: []string{"deploy/myapp"},
//	            Namespace:   "myapp",
//	            GitopsRepo:  "git@github.com:org/gitops.git",
//	            AppName:     "myapp",
//	        })
//	    })
package rollback

import (
	"context"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gitops"
	"github.com/sparkwing-dev/sparks-core/kube"
)

// Config configures a rollback. The kube fields drive the local path;
// the gitops fields drive the remote path. Populate both so the
// same config rolls back correctly regardless of where it runs.
type Config struct {
	// Deployments are the k8s deployments to roll back on the local
	// path (e.g. "deploy/myapp").
	Deployments []string
	// Namespace is the kubectl -n target for the local path.
	Namespace string
	// Context is the kubectl --context for the local path. Empty
	// resolves via kube.ResolveContext (explicit, or in-cluster
	// cluster, in-cluster) and fails closed.
	Context string
	// GitopsRepo is the gitops repo SSH URL for the remote path.
	GitopsRepo string
	// GitopsCommit is the commit to revert on the remote path. Defaults
	// to "HEAD" -- the most recent deploy.
	GitopsCommit string
	// AppName is the ArgoCD application to sync after a remote revert.
	AppName string
	// ArgoCD names the server the remote path syncs against and the
	// token it authenticates with. An empty Server probes the
	// in-cluster service.
	ArgoCD gitops.ArgoCDConfig
	// Local routes to the kubectl path instead of gitops.
	Local bool
}

// Run rolls back the most recent deployment using the path that matches
// the environment, mirroring deploy.Run's routing.
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
