package jobs

import (
	"reflect"
	"testing"
)

func TestLayeringViolation(t *testing.T) {
	cases := []struct {
		module string
		imp    string
		bad    bool
	}{
		{"docker", "github.com/sparkwing-dev/sparks-core/step", false},   // downward, ok
		{"docker", "github.com/sparkwing-dev/sparks-core/aws", false},    // downward (1->0), ok
		{"step", "github.com/sparkwing-dev/sparks-core/docker", true},    // upward (0->1)
		{"docker", "github.com/sparkwing-dev/sparks-core/kube", true},    // sideways (1->1)
		{"kube", "github.com/sparkwing-dev/sparks-core/pipelines", true}, // upward (1->3)
		{"pipelines", "github.com/sparkwing-dev/sparks-core/deploy", false},
		{"deploy", "github.com/sparkwing-dev/sparks-core/kube", false},
		{"docker", "github.com/sparkwing-dev/sparkwing/sparkwing", false}, // blocks may use the SDK
		{"docker", "fmt", false},
		{"templates", "github.com/sparkwing-dev/sparkwing/sparkwing", true}, // templates must be SDK-free
		{"templates", "github.com/sparkwing-dev/sparkwing/sparkwing/docker", true},
		{"templates", "go.yaml.in/yaml/v3", false},
		{"templates", "github.com/sparkwing-dev/sparks-core/docker", true}, // layer 0 can't import any sparks-core
		{".sparkwing", "github.com/sparkwing-dev/sparks-core/kube", false}, // unlayered module: skipped
	}
	for _, tc := range cases {
		got := layeringViolation(tc.module, tc.imp) != ""
		if got != tc.bad {
			t.Errorf("layeringViolation(%q, %q) bad=%v, want %v", tc.module, tc.imp, got, tc.bad)
		}
	}
}

func TestImportPaths(t *testing.T) {
	src := `package p
import (
	"fmt"
	sw "github.com/sparkwing-dev/sparkwing/sparkwing"
	"github.com/sparkwing-dev/sparks-core/step"
)
var _ = fmt.Sprint`
	got, err := importPaths([]byte(src))
	if err != nil {
		t.Fatalf("importPaths: %v", err)
	}
	want := []string{"fmt", "github.com/sparkwing-dev/sparkwing/sparkwing", "github.com/sparkwing-dev/sparks-core/step"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("importPaths = %v, want %v", got, want)
	}
}
