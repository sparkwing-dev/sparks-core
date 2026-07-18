package kube

import (
	"context"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// DeleteConfig drives Delete: a `kubectl delete` over manifest paths
// and/or named resources. It exists for idempotent teardown -- e.g.
// removing a canary Deployment+Service on both promote and abort -- so
// IgnoreNotFound lets a second teardown of an already-gone object still
// succeed.
type DeleteConfig struct {
	// Paths are files or directories passed to `kubectl delete -f`, one
	// delete per entry. Combine with Resources or use either alone.
	Paths []string
	// Resources are resource identifiers passed to `kubectl delete`
	// directly, e.g. "deploy/myapp-canary" or "service/myapp-canary".
	// One delete per entry.
	Resources []string
	// Namespace is the -n target. Defaults to "default".
	Namespace string
	// Context is the kubectl --context. Empty resolves via ResolveContext
	// (SPARKWING_KUBE_CONTEXT, kind cluster, in-cluster) and fails closed
	// rather than silently using the current kubeconfig context.
	Context string
	// IgnoreNotFound adds --ignore-not-found so deleting an object that is
	// already gone is a success, making the teardown idempotent.
	IgnoreNotFound bool
	// ExtraArgs are appended verbatim to every `kubectl delete` argv, an
	// escape hatch for the CLI's long tail (--wait=false to avoid blocking
	// on a stuck finalizer, --grace-period, --force, --cascade).
	ExtraArgs []string
	// DryRun forces echo mode for this call even when SPARKWING_DRY_RUN is
	// unset: the argv is logged and no delete is executed.
	DryRun bool
}

func (c *DeleteConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
}

// deletePathArgs builds the argv for `kubectl delete -f <path>`.
func deletePathArgs(namespace, path string, ignoreNotFound bool, extra []string) []string {
	args := []string{"delete", "-n", namespace, "-f", path}
	if ignoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	return append(args, extra...)
}

// deleteResourceArgs builds the argv for `kubectl delete <resource>`.
func deleteResourceArgs(namespace, resource string, ignoreNotFound bool, extra []string) []string {
	args := []string{"delete", resource, "-n", namespace}
	if ignoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	return append(args, extra...)
}

// Delete removes each configured manifest path and resource via
// `kubectl delete`. With IgnoreNotFound it is safe to call twice, which
// is why an abort path can tear down a canary the promote path may have
// already removed. Under SPARKWING_DRY_RUN it echoes the kubectl argv
// for each delete and returns success without contacting the cluster.
func Delete(ctx context.Context, cfg DeleteConfig) error {
	cfg.defaults()
	if len(cfg.Paths) == 0 && len(cfg.Resources) == 0 {
		return fmt.Errorf("kube.Delete: at least one path or resource is required")
	}
	if cfg.DryRun {
		ctx = withDryRun(ctx)
	}
	return step.Run(ctx, "delete (kubectl)", func(ctx context.Context) error {
		for _, p := range cfg.Paths {
			sparkwing.Info(ctx, "deleting -f %s", p)
			if err := kubectl(ctx, cfg.Context, deletePathArgs(cfg.Namespace, p, cfg.IgnoreNotFound, cfg.ExtraArgs)...); err != nil {
				return err
			}
		}
		for _, r := range cfg.Resources {
			sparkwing.Info(ctx, "deleting %s", r)
			if err := kubectl(ctx, cfg.Context, deleteResourceArgs(cfg.Namespace, r, cfg.IgnoreNotFound, cfg.ExtraArgs)...); err != nil {
				return err
			}
		}
		return nil
	})
}
