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

// dryRunKey marks a context so kubectl echoes instead of executing, the
// per-call equivalent of setting SPARKWING_DRY_RUN.
type dryRunKey struct{}

// withDryRun returns ctx marked for echo mode, so a single Delete/Scale
// call can opt into dry-run without setting the process-wide env var.
func withDryRun(ctx context.Context) context.Context {
	return context.WithValue(ctx, dryRunKey{}, true)
}

// dryRunEnabled reports whether echo mode is active for ctx: either the
// SPARKWING_DRY_RUN env var is set, or the context was marked per call.
func dryRunEnabled(ctx context.Context) bool {
	if os.Getenv(dryRunEnv) != "" {
		return true
	}
	on, _ := ctx.Value(dryRunKey{}).(bool)
	return on
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
//  3. otherwise -- an error. No silent current-context fallthrough.
//
// Which cluster a command targets is the caller's to state, so nothing
// here reads it from the environment. Case 2 is a fact about the
// machine rather than a choice, which is why it stays.
//
// An empty return string means "run kubectl without --context" (case
// 2); a non-empty string is passed as --context.
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
// context via ResolveContext. Every cloud-mutating kubectl invocation in
// this package goes through here so the context is always explicit and
// never silently the current one. Under dry-run (SPARKWING_DRY_RUN or a
// per-call withDryRun context) it echoes the resolved argv and returns
// success without contacting the cluster; the fail-closed context guard
// is relaxed there because nothing is executed, so a dry run never needs
// a configured context to succeed.
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
