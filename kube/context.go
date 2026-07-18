package kube

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// dryRunEnv, when non-empty, switches every cloud-mutating kube helper
// into echo mode: it logs the exact kubectl argv it would run and
// returns success without touching the cluster. This is what a
// template-verify run relies on to stay green with no reachable cluster.
const dryRunEnv = "SPARKWING_DRY_RUN"

// dryRunEnabled reports whether echo mode is active.
func dryRunEnabled() bool {
	return os.Getenv(dryRunEnv) != ""
}

// runKubectl resolves the context and either echoes the full kubectl
// argv (dry-run) or executes it. Cloud-mutating helpers route through
// here so a dry run stays green with no cluster. The echoed line carries
// the resolved --context best-effort; the fail-closed context guard is
// relaxed under dry-run because nothing is executed, so a dry run never
// needs a configured context to succeed.
func runKubectl(ctx context.Context, explicit string, args ...string) error {
	if dryRunEnabled() {
		full := args
		if kc, err := ResolveContext(explicit); err == nil && kc != "" {
			full = append([]string{"--context", kc}, args...)
		}
		sparkwing.Info(ctx, "[dry-run] kubectl %s", strings.Join(full, " "))
		return nil
	}
	return kubectl(ctx, explicit, args...)
}

// ResolveContext decides which kubectl context a command should target.
// It is the single policy point for every kubectl invocation in this
// package, and it fails closed: rather than letting a command fall
// through to whatever context happens to be current in the caller's
// kubeconfig (which may be a production cluster), it returns an error
// when no context can be determined.
//
// Resolution order:
//
//  1. explicit -- a Context passed by the caller wins.
//  2. in-cluster -- when running inside a pod, the service account is
//     used and no --context is needed (returns "", nil).
//  3. SPARKWING_KUBE_CONTEXT -- the "configure once" knob: set it once
//     (env, runner config, pipeline env) and every kube call honors it.
//  4. kind-<SPARKWING_KIND_CLUSTER> -- convenience for local kind runs.
//  5. SPARKWING_KUBE_ALLOW_CURRENT=1 -- explicit opt-in to the current
//     context (returns "", nil). The escape hatch for "I really do mean
//     whatever kubeconfig is active".
//  6. otherwise -- an error. No silent current-context fallthrough.
//
// An empty return string means "run kubectl without --context" (cases 2
// and 5); a non-empty string is passed as --context.
func ResolveContext(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if IsRunningInK8s() {
		return "", nil
	}
	if c := os.Getenv("SPARKWING_KUBE_CONTEXT"); c != "" {
		return c, nil
	}
	if kc := os.Getenv("SPARKWING_KIND_CLUSTER"); kc != "" {
		return "kind-" + kc, nil
	}
	if os.Getenv("SPARKWING_KUBE_ALLOW_CURRENT") == "1" {
		return "", nil
	}
	return "", fmt.Errorf("kube: refusing to run kubectl without an explicit context " +
		"(it would target the current kubeconfig context, which may be the wrong cluster). " +
		"Set the Context field, or SPARKWING_KUBE_CONTEXT, or SPARKWING_KIND_CLUSTER; " +
		"set SPARKWING_KUBE_ALLOW_CURRENT=1 to deliberately use the current context")
}

// contextArgs returns the ["--context", <ctx>] prefix for a kubectl
// command (or nil when no context is needed), or an error when the
// context can't be resolved. Use it for capture-style calls that go
// through sparkwing.Exec directly.
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

// kubectl runs `kubectl [--context <resolved>] args...`, resolving the
// context via ResolveContext. Every kubectl invocation in this package
// goes through here so the context is always explicit and never silently
// the current one.
func kubectl(ctx context.Context, explicit string, args ...string) error {
	ca, err := contextArgs(explicit)
	if err != nil {
		return err
	}
	return step.Exec(ctx, "kubectl", append(ca, args...)...)
}

// kubectlCapture is kubectl for the read path: it returns the command's
// trimmed stdout instead of streaming it. Same context resolution, so
// capture-style queries (get -o name, ...) also stay context-explicit.
func kubectlCapture(ctx context.Context, explicit string, args ...string) (string, error) {
	ca, err := contextArgs(explicit)
	if err != nil {
		return "", err
	}
	return sparkwing.Exec(ctx, "kubectl", append(ca, args...)...).String()
}
