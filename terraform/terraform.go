// Package terraform wraps the `terraform` CLI for sparkwing pipelines:
// initialize a working directory, plan to a saved plan file (parsing the
// add/change/destroy summary), and apply exactly that saved plan.
//
// The discipline this module enforces is plan-then-apply-the-saved-plan.
// Apply never re-plans; it applies the exact plan file Plan wrote. That
// closes the drift window between what a reviewer approved and what runs:
// a fresh plan taken at apply time could differ from the one that was
// reviewed if state or the world changed in between. PlanResult.PlanFile
// is the handle that carries the reviewed plan from Plan to Apply.
//
// It is cloud-agnostic: one `terraform` binary serves AWS, GCP, and any
// other provider, so the same block backs both AWS and GCP templates.
//
// Required host tool: `terraform` must be on PATH. State-backend access
// (credentials, backend config) is the caller's responsibility and comes
// from the ambient environment.
//
// Dry-run: Plan is state-reading (init + plan mutate no cloud resources)
// and always executes. Apply mutates real infrastructure, so it honors
// the SPARKWING_DRY_RUN convention: when SPARKWING_DRY_RUN is non-empty
// Apply logs the exact terraform argv it would run and returns success
// without executing.
package terraform

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// DefaultPlanFile is the saved plan filename Plan writes (relative to
// Config.Dir) when Config.PlanFile is empty.
const DefaultPlanFile = "tfplan"

// DefaultLockTimeout is the -lock-timeout applied to init, plan, and apply
// when Config.LockTimeout is empty. terraform's own default is 0s, which
// fails immediately when the state lock is held; a non-zero wait is safer
// for CI against a remote backend with concurrent pipelines.
const DefaultLockTimeout = "5m"

// Config drives both Plan and Apply. The same Config used to produce a
// plan should be passed to Apply so the working directory and workspace
// match the saved plan.
type Config struct {
	// Dir is the terraform root directory (where the .tf files and
	// backend config live). Defaults to ".".
	Dir string
	// VarFiles are tfvars files passed to plan as -var-file, in order.
	VarFiles []string
	// Vars are individual -var key=value pairs passed to plan. Keys are
	// applied in sorted order so the argv is deterministic.
	Vars map[string]string
	// Workspace, when set, is selected via `terraform workspace select`
	// after init and before planning. Empty uses the current workspace.
	// The workspace must already exist unless CreateWorkspace is set.
	Workspace string
	// CreateWorkspace, when true, selects the workspace with
	// `-or-create=true` so a missing workspace is created on first use
	// instead of erroring. Requires terraform 0.15.4+. Ignored when
	// Workspace is empty.
	CreateWorkspace bool
	// Backend are -backend-config key=value pairs passed to init, in
	// sorted-key order. Empty relies on the backend block as written.
	Backend map[string]string
	// PlanFile overrides the saved plan filename (relative to Dir).
	// Empty uses DefaultPlanFile.
	PlanFile string
	// LockTimeout is the -lock-timeout passed to init, plan, and apply.
	// Empty uses DefaultLockTimeout. Set "0s" to restore terraform's
	// fail-fast behavior when the state lock is held.
	LockTimeout string
	// InitArgs are extra flags appended to `terraform init` after the
	// fixed flags and backend config (for example -upgrade, -reconfigure).
	InitArgs []string
	// PlanArgs are extra flags appended to `terraform plan` after the
	// fixed flags, var-files, and vars (for example -target=..., -replace=...,
	// -refresh=false, -destroy).
	PlanArgs []string
	// ApplyArgs are extra flags appended to `terraform apply` before the
	// saved plan file (for example -parallelism=N). Flags that re-plan or
	// take -var/-var-file are rejected by terraform when applying a saved
	// plan.
	ApplyArgs []string
}

func (c *Config) dir() string {
	if c.Dir == "" {
		return "."
	}
	return c.Dir
}

func (c *Config) planFile() string {
	if c.PlanFile == "" {
		return DefaultPlanFile
	}
	return c.PlanFile
}

func (c *Config) lockTimeout() string {
	if c.LockTimeout == "" {
		return DefaultLockTimeout
	}
	return c.LockTimeout
}

// PlanResult reports the outcome of Plan: the parsed change counts, the
// human-readable summary line terraform printed, and the path (relative
// to Config.Dir) of the saved plan file to hand to Apply.
type PlanResult struct {
	Adds     int
	Changes  int
	Destroys int
	Summary  string
	PlanFile string
}

// Plan runs `terraform init`, optionally selects a workspace, then runs
// `terraform plan -out=<PlanFile>` and parses the add/change/destroy
// summary. Plan performs no cloud mutation, so it runs even when
// SPARKWING_DRY_RUN is set. The returned PlanResult.PlanFile is the saved
// plan to pass to Apply.
func Plan(ctx context.Context, cfg Config) (PlanResult, error) {
	res := PlanResult{PlanFile: cfg.planFile()}
	dir := cfg.dir()
	err := step.Run(ctx, "terraform plan", func(ctx context.Context) error {
		if _, err := sparkwing.Exec(ctx, "terraform", initArgs(cfg)...).Dir(dir).Run(); err != nil {
			return err
		}
		if cfg.Workspace != "" {
			if _, err := sparkwing.Exec(ctx, "terraform", workspaceSelectArgs(cfg)...).Dir(dir).Run(); err != nil {
				return err
			}
		}
		out, err := sparkwing.Exec(ctx, "terraform", planArgs(cfg)...).Dir(dir).Run()
		if err != nil {
			return err
		}
		s := ParseChangeSummary(out.Stdout)
		res.Adds, res.Changes, res.Destroys, res.Summary = s.Adds, s.Changes, s.Destroys, s.Summary
		sparkwing.Info(ctx, "plan summary: %s", res.Summary)
		return nil
	})
	return res, err
}

// ApplyOptions selects which saved plan Apply applies.
type ApplyOptions struct {
	// PlanFile is the saved plan (relative to Config.Dir) to apply. It is
	// normally PlanResult.PlanFile from a preceding Plan. Required.
	PlanFile string
}

// Apply applies exactly the saved plan named by opt.PlanFile via
// `terraform apply <planfile>` -- it never re-plans. terraform rejects
// -var/-var-file when applying a saved plan (the plan already encodes
// them), so none are passed here.
//
// Apply mutates real infrastructure. When SPARKWING_DRY_RUN is non-empty
// it logs the exact terraform argv and returns nil without executing.
func Apply(ctx context.Context, cfg Config, opt ApplyOptions) error {
	if opt.PlanFile == "" {
		return fmt.Errorf("terraform.Apply: PlanFile is required")
	}
	dir := cfg.dir()
	argv := applyArgs(cfg, opt)
	return step.Run(ctx, "terraform apply", func(ctx context.Context) error {
		if dryRun() {
			if cfg.Workspace != "" {
				echoDryRun(ctx, dir, workspaceSelectArgs(cfg))
			}
			echoDryRun(ctx, dir, argv)
			return nil
		}
		if cfg.Workspace != "" {
			if _, err := sparkwing.Exec(ctx, "terraform", workspaceSelectArgs(cfg)...).Dir(dir).Run(); err != nil {
				return err
			}
		}
		_, err := sparkwing.Exec(ctx, "terraform", argv...).Dir(dir).Run()
		return err
	})
}

// initArgs builds the `terraform init` argv for cfg.
func initArgs(cfg Config) []string {
	args := []string{"init", "-input=false", "-no-color", "-lock-timeout=" + cfg.lockTimeout()}
	for _, k := range sortedKeys(cfg.Backend) {
		args = append(args, "-backend-config="+k+"="+cfg.Backend[k])
	}
	return append(args, cfg.InitArgs...)
}

// planArgs builds the `terraform plan` argv for cfg.
func planArgs(cfg Config) []string {
	args := []string{"plan", "-input=false", "-no-color", "-lock-timeout=" + cfg.lockTimeout(), "-out=" + cfg.planFile()}
	for _, vf := range cfg.VarFiles {
		args = append(args, "-var-file="+vf)
	}
	for _, k := range sortedKeys(cfg.Vars) {
		args = append(args, "-var="+k+"="+cfg.Vars[k])
	}
	return append(args, cfg.PlanArgs...)
}

// applyArgs builds the `terraform apply` argv for a saved plan. The saved
// plan file is the final positional argument, so cfg.ApplyArgs and the
// fixed flags precede it.
func applyArgs(cfg Config, opt ApplyOptions) []string {
	args := []string{"apply", "-input=false", "-no-color", "-lock-timeout=" + cfg.lockTimeout()}
	args = append(args, cfg.ApplyArgs...)
	return append(args, opt.PlanFile)
}

// workspaceSelectArgs builds the `terraform workspace select` argv for
// cfg.Workspace, adding -or-create when CreateWorkspace is set.
func workspaceSelectArgs(cfg Config) []string {
	args := []string{"workspace", "select"}
	if cfg.CreateWorkspace {
		args = append(args, "-or-create=true")
	}
	return append(args, cfg.Workspace)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dryRun() bool {
	return os.Getenv("SPARKWING_DRY_RUN") != ""
}

// echoDryRun logs the terraform argv a mutating call would have run,
// honoring the SPARKWING_DRY_RUN convention.
func echoDryRun(ctx context.Context, dir string, argv []string) {
	sparkwing.Info(ctx, "SPARKWING_DRY_RUN set: would run (in %s): terraform %s", dir, joinArgs(argv))
}

func joinArgs(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
