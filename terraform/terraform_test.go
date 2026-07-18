package terraform

import (
	"context"
	"strings"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func eq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestInitArgs_BackendConfigSortedByKey(t *testing.T) {
	args := initArgs(Config{Backend: map[string]string{
		"region": "us-west-2",
		"bucket": "tfstate",
		"key":    "app/terraform.tfstate",
	}})
	eq(t, "initArgs", args, []string{
		"init", "-input=false", "-no-color",
		"-backend-config=bucket=tfstate",
		"-backend-config=key=app/terraform.tfstate",
		"-backend-config=region=us-west-2",
	})
}

func TestInitArgs_NoBackend(t *testing.T) {
	eq(t, "initArgs", initArgs(Config{}), []string{"init", "-input=false", "-no-color"})
}

func TestPlanArgs_DefaultPlanFileAndSortedVars(t *testing.T) {
	args := planArgs(Config{
		VarFiles: []string{"prod.tfvars", "secrets.tfvars"},
		Vars:     map[string]string{"region": "us-west-2", "env": "prod"},
	})
	eq(t, "planArgs", args, []string{
		"plan", "-input=false", "-no-color", "-out=tfplan",
		"-var-file=prod.tfvars", "-var-file=secrets.tfvars",
		"-var=env=prod", "-var=region=us-west-2",
	})
}

func TestPlanArgs_CustomPlanFile(t *testing.T) {
	args := planArgs(Config{PlanFile: "plan.bin"})
	eq(t, "planArgs", args, []string{"plan", "-input=false", "-no-color", "-out=plan.bin"})
}

func TestApplyArgs_ReferencesSavedPlan(t *testing.T) {
	eq(t, "applyArgs", applyArgs(ApplyOptions{PlanFile: "tfplan"}),
		[]string{"apply", "-input=false", "-no-color", "tfplan"})
}

func TestConfigDefaults_DirAndPlanFile(t *testing.T) {
	c := Config{}
	if c.dir() != "." {
		t.Fatalf("dir default = %q, want .", c.dir())
	}
	if c.planFile() != DefaultPlanFile {
		t.Fatalf("planFile default = %q, want %q", c.planFile(), DefaultPlanFile)
	}
	c2 := Config{Dir: "infra", PlanFile: "p.bin"}
	if c2.dir() != "infra" || c2.planFile() != "p.bin" {
		t.Fatalf("overrides not honored: dir=%q planFile=%q", c2.dir(), c2.planFile())
	}
}

func TestApply_RequiresPlanFile(t *testing.T) {
	if err := Apply(context.Background(), Config{}, ApplyOptions{}); err == nil {
		t.Fatal("Apply with empty PlanFile = nil, want error")
	}
}

type captureLogger struct{ msgs []string }

func (c *captureLogger) Log(_, msg string)            { c.msgs = append(c.msgs, msg) }
func (c *captureLogger) Emit(rec sparkwing.LogRecord) { c.msgs = append(c.msgs, rec.Msg) }

func withCapture(t *testing.T) (context.Context, *captureLogger) {
	t.Helper()
	lg := &captureLogger{}
	ctx := context.WithValue(context.Background(), sparkwing.RuntimePlumbing.Keys.Logger, sparkwing.Logger(lg))
	return ctx, lg
}

func TestApply_DryRunEchoesArgvWithoutExecuting(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	ctx, lg := withCapture(t)
	// terraform is not invoked here; a real exec of a saved plan against
	// no backend would error, so a nil return proves nothing ran.
	if err := Apply(ctx, Config{Dir: "infra"}, ApplyOptions{PlanFile: "tfplan"}); err != nil {
		t.Fatalf("dry-run Apply = %v, want nil", err)
	}
	var echoed string
	for _, m := range lg.msgs {
		if strings.Contains(m, "would run") {
			echoed = m
		}
	}
	if echoed == "" {
		t.Fatalf("no dry-run echo in %v", lg.msgs)
	}
	if !strings.Contains(echoed, "terraform apply -input=false -no-color tfplan") {
		t.Fatalf("echo missing exact argv: %q", echoed)
	}
	if !strings.Contains(echoed, "in infra") {
		t.Fatalf("echo missing working dir: %q", echoed)
	}
}
