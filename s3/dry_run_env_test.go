package s3

import (
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwingtest"
)

func TestDryRunBooleanEnvironment(t *testing.T) {
	for _, value := range []string{"", "false", "FALSE", "False", "f", "F", "0", "invalid", " true ", "1", "t", "T", "true", "TRUE", "True"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SPARKWING_DRY_RUN", value)
			want := value == "1" || value == "t" || value == "T" || value == "true" || value == "TRUE" || value == "True"
			if got := dryRun(); got != want {
				t.Errorf("dry run with env %q = %v, want %v", value, got, want)
			}
		})
	}
}

func TestDeployStaticSite_FalseDryRunExecutesCLI(t *testing.T) {
	host := sparkwingtest.New(sparkwingtest.Options{})
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	awsEnvOff(t)
	t.Setenv("SPARKWING_DRY_RUN", "false")
	logPath := fakeAWSCLI(t)
	if _, err := DeployStaticSite(host.Context(t.Context()), StaticSiteConfig{Bucket: "site", OutDir: siteDir(t)}); err != nil {
		t.Fatal(err)
	}
	if runs := recordedInvocations(t, logPath); len(runs) != 2 {
		t.Fatalf("aws invocations = %d, want asset sync and HTML copy", len(runs))
	}
}
