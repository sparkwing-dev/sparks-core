package kube

import (
	"context"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// ApplyConfig drives Apply: a raw `kubectl apply` against one or more
// manifest paths. Use this for repos that own plain k8s YAML (no
// kustomize, no gitops) and want the pipeline to apply it directly.
type ApplyConfig struct {
	// Paths are files or directories passed to `kubectl apply -f`.
	// Required.
	Paths []string
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context. Empty uses the current context
	// (or the in-cluster service account when running in a pod).
	Context string
	// ServerSide applies with --server-side, the safer choice for large
	// or CRD-heavy manifests.
	ServerSide bool
	// Wait lists deployment names (e.g. "deploy/myapp") to block on via
	// `kubectl rollout status` after the apply. Empty waits on nothing.
	Wait []string
	// Timeout bounds each rollout-status wait. Defaults to "180s".
	Timeout string
}

func (c *ApplyConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.Timeout == "" {
		c.Timeout = "180s"
	}
}

func (c ApplyConfig) ctxArgs() []string {
	if c.Context != "" {
		return []string{"--context", c.Context}
	}
	return nil
}

// Apply runs `kubectl apply` for each configured path, then waits for
// any listed deployments to finish rolling out. A plain-YAML deploy
// path, parallel to DeployKindKustomize (kustomize) and DeployKubectl
// (rollout restart).
func Apply(ctx context.Context, cfg ApplyConfig) error {
	cfg.defaults()
	if len(cfg.Paths) == 0 {
		return fmt.Errorf("kube.Apply: at least one path is required")
	}
	return step.Run(ctx, "apply (kubectl)", func(ctx context.Context) error {
		base := cfg.ctxArgs()
		for _, p := range cfg.Paths {
			args := append([]string{}, base...)
			args = append(args, "apply", "-n", cfg.Namespace, "-f", p)
			if cfg.ServerSide {
				args = append(args, "--server-side")
			}
			sparkwing.Info(ctx, "applying %s", p)
			if err := step.Exec(ctx, "kubectl", args...); err != nil {
				return err
			}
		}
		for _, deploy := range cfg.Wait {
			args := append([]string{}, base...)
			args = append(args, "rollout", "status", deploy, "-n", cfg.Namespace, "--timeout="+cfg.Timeout)
			sparkwing.Info(ctx, "waiting for %s rollout", deploy)
			if err := step.Exec(ctx, "kubectl", args...); err != nil {
				return err
			}
		}
		return nil
	})
}

// RolloutUndo rolls each deployment back to its previous ReplicaSet via
// `kubectl rollout undo`, then waits for the rollback to complete. It is
// the kubectl-side rollback primitive: pair it with a failed Verify in
// an OnFailure handler, or call it from the rollback dispatcher.
func RolloutUndo(ctx context.Context, deployments []string, namespace string) error {
	if namespace == "" {
		namespace = "default"
	}
	return step.Run(ctx, "rollback (kubectl rollout undo)", func(ctx context.Context) error {
		for _, deploy := range deployments {
			sparkwing.Info(ctx, "rolling back %s", deploy)
			if err := step.Exec(ctx, "kubectl", "rollout", "undo", deploy, "-n", namespace); err != nil {
				return err
			}
			if err := step.Exec(ctx, "kubectl", "rollout", "status", deploy, "-n", namespace, "--timeout=180s"); err != nil {
				return err
			}
		}
		return nil
	})
}
