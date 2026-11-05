package gcp

import "testing"

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
