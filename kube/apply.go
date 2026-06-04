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
	// Context is the kubectl --context. Empty resolves via ResolveContext
	// (SPARKWING_KUBE_CONTEXT, kind cluster, in-cluster) and fails closed
	// rather than silently using the current kubeconfig context.
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
		for _, p := range cfg.Paths {
			args := []string{"apply", "-n", cfg.Namespace, "-f", p}
			if cfg.ServerSide {
				args = append(args, "--server-side")
			}
			sparkwing.Info(ctx, "applying %s", p)
			if err := kubectl(ctx, cfg.Context, args...); err != nil {
				return err
			}
		}
		for _, deploy := range cfg.Wait {
			sparkwing.Info(ctx, "waiting for %s rollout", deploy)
			if err := kubectl(ctx, cfg.Context, "rollout", "status", deploy, "-n", cfg.Namespace, "--timeout="+cfg.Timeout); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetImageConfig drives SetImage.
type SetImageConfig struct {
	// Deployment is the rollout target, e.g. "deploy/myapp". Required.
	Deployment string
	// Container is the container name within the pod spec to retag.
	// Required.
	Container string
	// Image is the full image reference (registry/name:tag) to roll to.
	// Required.
	Image string
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context. Empty resolves via ResolveContext
	// and fails closed rather than using the current kubeconfig context.
	Context string
	// Timeout bounds the rollout-status wait. Defaults to "180s".
	Timeout string
}

func (c *SetImageConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.Timeout == "" {
		c.Timeout = "180s"
	}
}

// SetImage points a deployment's container at a new image via
// `kubectl set image` and waits for the resulting rollout. Each distinct
// image tag is a new ReplicaSet, so RolloutUndo can roll back to the
// prior tag -- which is why a CD pipeline should set the freshly built,
// content-addressed tag here rather than re-applying a floating :latest.
func SetImage(ctx context.Context, cfg SetImageConfig) error {
	cfg.defaults()
	if cfg.Deployment == "" || cfg.Container == "" || cfg.Image == "" {
		return fmt.Errorf("kube.SetImage: Deployment, Container, and Image are required")
	}
	return step.Run(ctx, "set image (kubectl)", func(ctx context.Context) error {
		sparkwing.Info(ctx, "%s %s=%s", cfg.Deployment, cfg.Container, cfg.Image)
		if err := kubectl(ctx, cfg.Context, "set", "image", cfg.Deployment, cfg.Container+"="+cfg.Image, "-n", cfg.Namespace); err != nil {
			return err
		}
		return kubectl(ctx, cfg.Context, "rollout", "status", cfg.Deployment, "-n", cfg.Namespace, "--timeout="+cfg.Timeout)
	})
}

// RolloutUndoConfig drives RolloutUndo.
type RolloutUndoConfig struct {
	// Deployments to roll back, e.g. "deploy/myapp". Required.
	Deployments []string
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context, ideally the same one the deploy
	// used. Empty resolves via ResolveContext (SPARKWING_KUBE_CONTEXT,
	// kind cluster, in-cluster) and fails closed -- so a rollback never
	// silently targets a different (e.g. production) cluster than the
	// deploy did.
	Context string
	// Timeout bounds each rollout-status wait. Defaults to "180s".
	Timeout string
}

func (c *RolloutUndoConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.Timeout == "" {
		c.Timeout = "180s"
	}
}

// RolloutUndo rolls each deployment back to its previous ReplicaSet via
// `kubectl rollout undo`, then waits for the rollback to complete. It is
// the kubectl-side rollback primitive: pair it with a failed Verify in
// an OnFailure handler, or call it from the rollback dispatcher.
func RolloutUndo(ctx context.Context, cfg RolloutUndoConfig) error {
	cfg.defaults()
	return step.Run(ctx, "rollback (kubectl rollout undo)", func(ctx context.Context) error {
		for _, deploy := range cfg.Deployments {
			sparkwing.Info(ctx, "rolling back %s", deploy)
			if err := kubectl(ctx, cfg.Context, "rollout", "undo", deploy, "-n", cfg.Namespace); err != nil {
				return err
			}
			if err := kubectl(ctx, cfg.Context, "rollout", "status", deploy, "-n", cfg.Namespace, "--timeout="+cfg.Timeout); err != nil {
				return err
			}
		}
		return nil
	})
}
