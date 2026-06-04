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
	// Context is the kubectl --context. Empty uses the current context.
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
	var ctxArgs []string
	if cfg.Context != "" {
		ctxArgs = []string{"--context", cfg.Context}
	}
	return step.Run(ctx, "set image (kubectl)", func(ctx context.Context) error {
		sparkwing.Info(ctx, "%s %s=%s", cfg.Deployment, cfg.Container, cfg.Image)
		set := append([]string{}, ctxArgs...)
		set = append(set, "set", "image", cfg.Deployment, cfg.Container+"="+cfg.Image, "-n", cfg.Namespace)
		if err := step.Exec(ctx, "kubectl", set...); err != nil {
			return err
		}
		status := append([]string{}, ctxArgs...)
		status = append(status, "rollout", "status", cfg.Deployment, "-n", cfg.Namespace, "--timeout="+cfg.Timeout)
		return step.Exec(ctx, "kubectl", status...)
	})
}

// RolloutUndoConfig drives RolloutUndo.
type RolloutUndoConfig struct {
	// Deployments to roll back, e.g. "deploy/myapp". Required.
	Deployments []string
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context. Empty uses the current context.
	//
	// Set this to the same context the deploy used. A rollback that
	// omits the context targets whatever kubeconfig context happens to
	// be current -- which may be a different (e.g. production) cluster
	// than the one just deployed to.
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
	var ctxArgs []string
	if cfg.Context != "" {
		ctxArgs = []string{"--context", cfg.Context}
	}
	return step.Run(ctx, "rollback (kubectl rollout undo)", func(ctx context.Context) error {
		for _, deploy := range cfg.Deployments {
			sparkwing.Info(ctx, "rolling back %s", deploy)
			undo := append(append([]string{}, ctxArgs...), "rollout", "undo", deploy, "-n", cfg.Namespace)
			if err := step.Exec(ctx, "kubectl", undo...); err != nil {
				return err
			}
			status := append(append([]string{}, ctxArgs...), "rollout", "status", deploy, "-n", cfg.Namespace, "--timeout="+cfg.Timeout)
			if err := step.Exec(ctx, "kubectl", status...); err != nil {
				return err
			}
		}
		return nil
	})
}
