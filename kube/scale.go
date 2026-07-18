package kube

import (
	"context"
	"fmt"
	"strconv"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// ScaleConfig drives Scale: `kubectl scale` a deployment to a replica
// count, then wait for the resulting rollout. It widens or narrows a
// slice of traffic -- e.g. scale a canary Deployment up before probing
// it, or down to zero before deleting it.
type ScaleConfig struct {
	// Deployment is the scale target, e.g. "deploy/myapp-canary".
	// Required.
	Deployment string
	// Replicas is the desired replica count. Zero is valid and narrows
	// the deployment to no pods.
	Replicas int
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context. Empty resolves via ResolveContext
	// and fails closed rather than using the current kubeconfig context.
	Context string
	// Timeout bounds the rollout-status wait. Defaults to "180s".
	Timeout string
}

func (c *ScaleConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.Timeout == "" {
		c.Timeout = "180s"
	}
}

// scaleArgs builds the argv for `kubectl scale <deployment> --replicas=N`.
func scaleArgs(namespace, deployment string, replicas int) []string {
	return []string{"scale", deployment, "--replicas=" + strconv.Itoa(replicas), "-n", namespace}
}

// rolloutStatusArgs builds the argv for a bounded rollout-status wait.
func rolloutStatusArgs(namespace, deployment, timeout string) []string {
	return []string{"rollout", "status", deployment, "-n", namespace, "--timeout=" + timeout}
}

// Scale sets a deployment's replica count via `kubectl scale` and waits
// for the rollout to settle. Under SPARKWING_DRY_RUN it echoes the
// kubectl argv and returns success without contacting the cluster.
func Scale(ctx context.Context, cfg ScaleConfig) error {
	cfg.defaults()
	if cfg.Deployment == "" {
		return fmt.Errorf("kube.Scale: Deployment is required")
	}
	if cfg.Replicas < 0 {
		return fmt.Errorf("kube.Scale: Replicas must not be negative")
	}
	return step.Run(ctx, "scale (kubectl)", func(ctx context.Context) error {
		sparkwing.Info(ctx, "scaling %s to %d replicas", cfg.Deployment, cfg.Replicas)
		if err := runKubectl(ctx, cfg.Context, scaleArgs(cfg.Namespace, cfg.Deployment, cfg.Replicas)...); err != nil {
			return err
		}
		return runKubectl(ctx, cfg.Context, rolloutStatusArgs(cfg.Namespace, cfg.Deployment, cfg.Timeout)...)
	})
}
