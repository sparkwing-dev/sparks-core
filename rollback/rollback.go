// Package rollback reverts the most recent deployment. It is the
// recovery counterpart to the deploy package and routes the same way:
//
//   - kind / local: `kubectl rollout undo` on the named deployments.
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
	"os"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gitops"
	"github.com/sparkwing-dev/sparks-core/kube"
)

// Config configures a rollback. The kube fields drive the local/kind
// path; the gitops fields drive the remote path. Populate both so the
// same config rolls back correctly regardless of where it runs.
type Config struct {
	// Deployments are the k8s deployments to roll back on the
	// local/kind path (e.g. "deploy/myapp").
	Deployments []string
	// Namespace is the kubectl -n target for the local/kind path.
	Namespace string
	// GitopsRepo is the gitops repo SSH URL for the remote path.
	GitopsRepo string
	// GitopsCommit is the commit to revert on the remote path. Defaults
	// to "HEAD" -- the most recent deploy.
	GitopsCommit string
	// AppName is the ArgoCD application to sync after a remote revert.
	AppName string
	// Local forces the kubectl path even when no kind cluster env hint
	// is present.
	Local bool
}

// Run rolls back the most recent deployment using the path that matches
// the environment, mirroring deploy.Run's routing.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Local || os.Getenv("SPARKWING_KIND_CLUSTER") != "" {
		sparkwing.Info(ctx, "rollback: local/kind -> kubectl rollout undo (ns=%s)", cfg.Namespace)
		return kube.RolloutUndo(ctx, cfg.Deployments, cfg.Namespace)
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
		return gitops.SyncArgoCD(ctx, cfg.AppName)
	}
	sparkwing.Info(ctx, "rollback: nothing reverted - skipping argocd sync")
	return nil
}
