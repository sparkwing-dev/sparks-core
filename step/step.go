// Package step is a small shared helper used across sparks-core
// modules. It does two things:
//
//  1. Step banner: a one-line log prefix so consumers visually see
//     where one chunk of work ends and the next begins when reading
//     pipeline logs.
//  2. Error-wrapped shell / exec helpers that do not panic. The pre-
//     rewrite SDK had must.Sh / must.Exec which panicked on failure
//     and relied on RunStep's deferred recover() to turn that into a
//     returned error. The new SDK doesn't ship RunStep, so the panic
//     trick doesn't fit; explicit error returns replace it.
//
// step is its own published module so that other sparks-core modules
// (gitops, kube, docker, ...) can depend on it without crossing Go's
// internal-import boundary. End-user consumers usually have no reason
// to depend on step directly -- prefer the higher-level sparks-core
// modules (pipelines, deploy, etc.) which call step internally.
package step

import (
	"context"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// Run logs a "==> name" banner and runs fn. Any error fn returns
// propagates unchanged so callers can chain multiple steps without
// losing the original error.
func Run(ctx context.Context, name string, fn func(context.Context) error) error {
	sparkwing.Info(ctx, "==> %s", name)
	return fn(ctx)
}

// Sh runs a shell line and returns the resulting error (if any),
// discarding the ExecResult. The line is passed to bash verbatim;
// dynamic values must come through env (use sparkwing.Bash(...).Env()
// directly when you need that). For dynamic argv, use step.Exec.
func Sh(ctx context.Context, line string) error {
	_, err := sparkwing.Bash(ctx, line).Run()
	return err
}

// Exec runs a command and returns the resulting error (if any),
// discarding the ExecResult.
func Exec(ctx context.Context, name string, args ...string) error {
	_, err := sparkwing.Exec(ctx, name, args...).Run()
	return err
}

// Debug logs at level "debug" via the pipeline logger. Thin wrapper
// over sparkwing.Debug kept for sparks-core's historical call sites.
func Debug(ctx context.Context, format string, a ...any) {
	sparkwing.Debug(ctx, format, a...)
}
