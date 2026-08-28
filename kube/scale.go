package kube

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

type ScaleConfig struct {
	// Deployment is required.
	Deployment string
	// Replicas zero is valid and narrows the deployment to no pods.
	Replicas int
	// Namespace defaults to "default".
	Namespace string
	// Context empty resolves via ResolveContext.
	Context string
	// Timeout defaults to "180s".
	Timeout string
	// ExtraArgs are appended verbatim to the scale argv; --current-replicas
	// guards a widen or narrow against a concurrent scale.
	ExtraArgs []string
	// DryRun forces echo mode even when SPARKWING_DRY_RUN is unset.
	DryRun bool
}

func (c *ScaleConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.Timeout == "" {
		c.Timeout = "180s"
	}
}

func scaleArgs(namespace, deployment string, replicas int, extra []string) []string {
	args := []string{"scale", deployment, "--replicas=" + strconv.Itoa(replicas), "-n", namespace}
	return append(args, extra...)
}

func rolloutStatusArgs(namespace, deployment, timeout string) []string {
	return []string{"rollout", "status", deployment, "-n", namespace, "--timeout=" + timeout}
}

// Scale sets a deployment's replica count and waits for the rollout to settle.
func Scale(ctx context.Context, cfg ScaleConfig) error {
	cfg.defaults()
	if cfg.Deployment == "" {
		return fmt.Errorf("kube.Scale: Deployment is required")
	}
	if cfg.Replicas < 0 {
		return fmt.Errorf("kube.Scale: Replicas must not be negative")
	}
	if cfg.DryRun {
		ctx = withDryRun(ctx)
	}
	return step.Run(ctx, "scale (kubectl)", func(ctx context.Context) error {
		sparkwing.Info(ctx, "scaling %s to %d replicas", cfg.Deployment, cfg.Replicas)
		if err := kubectl(ctx, cfg.Context, scaleArgs(cfg.Namespace, cfg.Deployment, cfg.Replicas, cfg.ExtraArgs)...); err != nil {
			return err
		}
		return kubectl(ctx, cfg.Context, rolloutStatusArgs(cfg.Namespace, cfg.Deployment, cfg.Timeout)...)
	})
}
