package kube

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

const dryRunEnv = "SPARKWING_DRY_RUN"

type dryRunKey struct{}

func withDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

func dryRunEnabled(ctx context.Context) bool {
	if os.Getenv(dryRunEnv) != "" {
		return true
	}
	on, _ := ctx.Value(dryRunKey{}).(bool)
	return on
}

// ResolveContext returns the kubectl context a command should target:
// explicit if given, or "" (no --context, the pod service account) when
// running in-cluster. Anything else is an error rather than a fallthrough
// to the caller's current kubeconfig context, which may be production.
func ResolveContext(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if IsRunningInK8s() {
		return "", nil
	}
	return "", fmt.Errorf("kube: refusing to run kubectl without an explicit context " +
		"(it would target the current kubeconfig context, which may be the wrong cluster). " +
		"Set the Context field on the config you are passing")
}

func contextArgs(explicit string) ([]string, error) {
	kc, err := ResolveContext(explicit)
	if err != nil {
		return nil, err
	}
	if kc == "" {
		return nil, nil
	}
	return []string{"--context", kc}, nil
}

// kubectl is the one path every mutating invocation takes, so the context
// is always explicit and never silently the current one. Dry-run relaxes the
// fail-closed context guard, because nothing is executed.
func kubectl(ctx context.Context, explicit string, args ...string) error {
	if dryRunEnabled(ctx) {
		full := args
		if kc, err := ResolveContext(explicit); err == nil && kc != "" {
			full = append([]string{"--context", kc}, args...)
		}
		sparkwing.Info(ctx, "[dry-run] kubectl %s", strings.Join(full, " "))
		return nil
	}
	ca, err := contextArgs(explicit)
	if err != nil {
		return err
	}
	return step.Exec(ctx, "kubectl", append(ca, args...)...)
}

// kubectlCapture is kubectl for the read path, returning trimmed stdout.
func kubectlCapture(ctx context.Context, explicit string, args ...string) (string, error) {
	ca, err := contextArgs(explicit)
	if err != nil {
		return "", err
	}
	return sparkwing.Exec(ctx, "kubectl", append(ca, args...)...).String()
}
