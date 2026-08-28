package kube

import (
	"context"
	"fmt"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

type DeleteConfig struct {
	// Paths are files or directories for `kubectl delete -f`, one delete
	// per entry.
	Paths []string
	// Resources are identifiers deleted directly, one delete per entry.
	Resources []string
	// Namespace defaults to "default".
	Namespace string
	// Context empty resolves via ResolveContext.
	Context string
	// IgnoreNotFound makes deleting an already-gone object a success, which
	// is what lets an abort path tear down what promote may have removed.
	IgnoreNotFound bool
	// ExtraArgs are appended verbatim to every delete argv.
	ExtraArgs []string
	// DryRun forces echo mode even when SPARKWING_DRY_RUN is unset.
	DryRun bool
}

func (c *DeleteConfig) defaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
}

func deletePathArgs(namespace, path string, ignoreNotFound bool, extra []string) []string {
	args := []string{"delete", "-n", namespace, "-f", path}
	if ignoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	return append(args, extra...)
}

func deleteResourceArgs(namespace, resource string, ignoreNotFound bool, extra []string) []string {
	args := []string{"delete", resource, "-n", namespace}
	if ignoreNotFound {
		args = append(args, "--ignore-not-found")
	}
	return append(args, extra...)
}

// Delete removes each configured manifest path and resource.
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
