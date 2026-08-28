package kube

import (
	"context"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// ApplyConfig drives Apply, for repos that own plain k8s YAML.
type ApplyConfig struct {
	// Paths are files or directories for `kubectl apply -f`. Required.
	Paths []string
	// Namespace defaults to "default".
	Namespace string
	// Context empty resolves via ResolveContext.
	Context string
	// ServerSide is the safer choice for large or CRD-heavy manifests.
	ServerSide bool
	// Wait names deployments to block on via `kubectl rollout status`.
	Wait []string
	// Timeout defaults to "180s".
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

// Apply runs `kubectl apply` for each path, then waits on any listed
// deployments.
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

type SetImageConfig struct {
	// Deployment, Container, and Image are required.
	Deployment string
	Container  string
	Image      string
	// Namespace defaults to "default".
	Namespace string
	// Context empty resolves via ResolveContext.
	Context string
	// Timeout defaults to "180s".
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

// SetImage points a deployment's container at a new image and waits for the
// rollout. Pass a content-addressed tag, not a floating :latest: each
// distinct tag is a new ReplicaSet, which is what RolloutUndo rolls back to.
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

type RolloutUndoConfig struct {
	// Deployments to roll back. Required.
	Deployments []string
	// Namespace defaults to "default".
	Namespace string
	// Context should be the one the deploy used; empty resolves via
	// ResolveContext, which fails closed rather than target another cluster.
	Context string
	// Timeout defaults to "180s".
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

// RolloutUndo rolls each deployment back to its previous ReplicaSet and
// waits for the rollback to complete.
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
