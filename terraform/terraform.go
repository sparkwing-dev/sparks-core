// Package terraform wraps the `terraform` CLI: init a working directory,
// plan to a saved plan file, and apply exactly that file. Apply never
// re-plans, which closes the drift window between what a reviewer approved
// and what runs. Plan always executes; Apply honors SPARKWING_DRY_RUN.
package terraform

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// DefaultPlanFile is the saved plan filename, relative to Config.Dir.
const DefaultPlanFile = "tfplan"

// DefaultLockTimeout replaces terraform's own 0s, which fails immediately
// when the state lock is held by a concurrent pipeline.
const DefaultLockTimeout = "5m"

// Config drives both Plan and Apply. Pass the same Config to both so the
// working directory and workspace match the saved plan.
type Config struct {
	// Dir is the terraform root directory, defaulting to ".".
	Dir string
	// VarFiles become -var-file flags on plan, in order.
	VarFiles []string
	// Vars become -var flags on plan, sorted by key for a deterministic argv.
	Vars map[string]string
	// Workspace is selected after init. It must already exist unless
	// CreateWorkspace is set.
	Workspace string
	// CreateWorkspace selects with -or-create=true, needing terraform 0.15.4+.
	CreateWorkspace bool
	// Backend become -backend-config flags on init, sorted by key.
	Backend map[string]string
	// PlanFile defaults to DefaultPlanFile.
	PlanFile string
	// LockTimeout defaults to DefaultLockTimeout; "0s" restores terraform's
	// fail-fast behavior.
	LockTimeout string
	InitArgs    []string
	PlanArgs    []string
	// ApplyArgs precede the saved plan file. terraform rejects flags that
	// re-plan or take -var/-var-file when applying one.
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

// PlanResult carries the parsed change counts, terraform's summary line, and
// the saved plan file to hand to Apply.
type PlanResult struct {
	Adds     int
	Changes  int
	Destroys int
	Summary  string
	PlanFile string
}

// Plan inits, selects any workspace, plans to a saved plan file, and parses
// the add/change/destroy summary.
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

type ApplyOptions struct {
	// PlanFile is required, normally PlanResult.PlanFile from a preceding Plan.
	PlanFile string
}

// Apply applies exactly the saved plan named by opt.PlanFile, never
// re-planning. It honors SPARKWING_DRY_RUN.
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

func initArgs(cfg Config) []string {
	args := []string{"init", "-input=false", "-no-color", "-lock-timeout=" + cfg.lockTimeout()}
	for _, k := range sortedKeys(cfg.Backend) {
		args = append(args, "-backend-config="+k+"="+cfg.Backend[k])
	}
	return append(args, cfg.InitArgs...)
}

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

func applyArgs(cfg Config, opt ApplyOptions) []string {
	args := []string{"apply", "-input=false", "-no-color", "-lock-timeout=" + cfg.lockTimeout()}
	args = append(args, cfg.ApplyArgs...)
	return append(args, opt.PlanFile)
}

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
