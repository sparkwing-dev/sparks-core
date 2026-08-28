// Package step logs step banners and wraps the shell and exec helpers to
// return errors rather than panic. It is published separately so the other
// sparks-core modules can depend on it without crossing Go's internal-import
// boundary; consumers should prefer those higher-level modules.
package step

import (
	"context"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// Run logs a "==> name" banner and runs fn, propagating its error unchanged.
func Run(ctx context.Context, name string, fn func(context.Context) error) error {
	sparkwing.Info(ctx, "==> %s", name)
	return fn(ctx)
}

// Sh runs a shell line verbatim, discarding the ExecResult. Dynamic values
// must come through sparkwing.Bash(...).Env(); dynamic argv belongs in Exec.
func Sh(ctx context.Context, line string) error {
	_, err := sparkwing.Bash(ctx, line).Run()
	return err
}

// Exec runs a command, discarding the ExecResult.
func Exec(ctx context.Context, name string, args ...string) error {
	_, err := sparkwing.Exec(ctx, name, args...).Run()
	return err
}

// Debug logs at level "debug" via the pipeline logger.
func Debug(ctx context.Context, format string, a ...any) {
	sparkwing.Debug(ctx, format, a...)
}
