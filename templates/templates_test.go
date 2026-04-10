package templates

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestList_AllTemplatesLoadable is the top-level smoke check: every
// template registered in templateNames has a parseable manifest, a
// non-empty body, and a non-empty README. Catches authoring mistakes
// (missing files, manifest-name drift) at PR review time rather than
// at first user encounter.
func TestList_AllTemplatesLoadable(t *testing.T) {
	all, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("List: no templates returned")
	}
	for _, tmpl := range all {
		if tmpl.Manifest.Name == "" {
			t.Errorf("template with empty manifest name (body prefix: %q)", head(tmpl.Body, 40))
		}
		if tmpl.Manifest.Description == "" {
			t.Errorf("%s: empty description", tmpl.Manifest.Name)
		}
		if tmpl.Body == "" {
			t.Errorf("%s: empty pipeline.go.tmpl body", tmpl.Manifest.Name)
		}
		if tmpl.ReadMe == "" {
			t.Errorf("%s: empty README.md", tmpl.Manifest.Name)
		}
	}
}

func TestGet_UnknownReturnsErrNotExist(t *testing.T) {
	_, err := Get("does-not-exist")
	if err == nil {
		t.Fatal("Get: expected error for unknown template")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Get: error should wrap fs.ErrNotExist, got %v", err)
	}
}

func TestRender_RequiresRequiredParams(t *testing.T) {
	// static-deploy-s3-cloudfront has bucket + distribution as
	// required. Calling Render with no params must surface both.
	_, err := Render("static-deploy-s3-cloudfront", map[string]string{})
	if err == nil {
		t.Fatal("Render: expected error when required params missing")
	}
	msg := err.Error()
	if !strings.Contains(msg, "bucket") {
		t.Errorf("Render: error should mention missing 'bucket': %v", err)
	}
}

func TestRender_RejectsUnknownParam(t *testing.T) {
	_, err := Render("lint-test-go", map[string]string{
		"go-version":      "1.26",
		"not-a-real-knob": "oops",
	})
	if err == nil {
		t.Fatal("Render: expected error for unknown param")
	}
	if !strings.Contains(err.Error(), "not-a-real-knob") {
		t.Errorf("Render: error should name unknown param, got %v", err)
	}
}

func TestRender_StaticDeployS3CloudFront_Substitutes(t *testing.T) {
	out, err := Render("static-deploy-s3-cloudfront", map[string]string{
		"bucket":       "my-test-bucket",
		"distribution": "E2ABCDEF",
		"url":          "https://example.com",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// Hard substitutions land verbatim in the rendered body.
	for _, want := range []string{
		`"my-test-bucket"`,
		`"E2ABCDEF"`,
		`"https://example.com"`,
		`package jobs`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render: missing %q in output\n--- output ---\n%s", want, out)
		}
	}
}

func TestRender_DockerDeployECR_Substitutes(t *testing.T) {
	out, err := Render("docker-deploy-ecr-eks", map[string]string{
		"image":       "my-app",
		"ecr":         "1234.dkr.ecr.us-west-2.amazonaws.com",
		"gitops-repo": "git@github.com:org/gitops.git",
		"gitops-path": "apps/my-app",
		"app-name":    "my-app",
		"namespace":   "my-app",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`"my-app"`,
		`"1234.dkr.ecr.us-west-2.amazonaws.com"`,
		`"git@github.com:org/gitops.git"`,
		`package jobs`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render: missing %q in output\n--- output ---\n%s", want, out)
		}
	}
}

// TestRender_DockerDeployGAR_TestCmdEmpty exercises the conditional
// branch in docker-deploy-gar-gke where test-cmd="" elides the test
// node entirely. The rendered Go must still be parseable.
func TestRender_DockerDeployGAR_TestCmdEmpty(t *testing.T) {
	out, err := Render("docker-deploy-gar-gke", map[string]string{
		"image":       "x",
		"gar":         "us-west1-docker.pkg.dev/p/r",
		"gitops-repo": "git@github.com:o/g.git",
		"gitops-path": "x",
		"app-name":    "x",
		"namespace":   "x",
		"test-cmd":    "", // explicit empty -- elide the test node
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// With test-cmd empty, the rendered body must NOT have a `test`
	// node call. Otherwise we'd wire up a step that ran nothing.
	if strings.Contains(out, `"test"`) {
		t.Errorf("expected no `test` node when test-cmd empty, got:\n%s", out)
	}
}

func TestRender_LintTestGo_DefaultsApplied(t *testing.T) {
	// lint-test-go has go-version with a default; rendering with no
	// params should still succeed.
	out, err := Render("lint-test-go", map[string]string{})
	if err != nil {
		t.Fatalf("Render with defaults: %v", err)
	}
	if !strings.Contains(out, "package jobs") {
		t.Errorf("Render: output missing package decl:\n%s", out)
	}
}

// TestRender_AllTemplatesProduceParseableGo checks that every
// registered template, supplied with its required parameters (any
// reasonable placeholder values), renders to syntactically valid Go.
// Catches accidental template-syntax bugs (`{{ if .foo }}` without a
// matching `{{ end }}`, mistyped field names, etc.) before the CLI
// hands the output to the user.
func TestRender_AllTemplatesProduceParseableGo(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]string
	}{
		{"static-deploy-s3-cloudfront", map[string]string{
			"bucket":       "test-bucket",
			"distribution": "EXXX",
		}},
		{"static-deploy-gcs-cloudcdn", map[string]string{
			"bucket":  "test-bucket",
			"url-map": "test-url-map",
			"project": "test-project",
		}},
		{"docker-deploy-ecr-eks", map[string]string{
			"image":       "test-app",
			"ecr":         "1234.dkr.ecr.us-west-2.amazonaws.com",
			"gitops-repo": "git@github.com:org/gitops.git",
			"gitops-path": "apps/test",
			"app-name":    "test-app",
			"namespace":   "test",
		}},
		{"docker-deploy-gar-gke", map[string]string{
			"image":       "test-app",
			"gar":         "us-west1-docker.pkg.dev/proj/repo",
			"gitops-repo": "git@github.com:org/gitops.git",
			"gitops-path": "apps/test",
			"app-name":    "test-app",
			"namespace":   "test",
		}},
		{"next-build-and-push", map[string]string{
			"artifact-bucket": "test-artifacts",
		}},
		{"lint-test-go", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Render(tc.name, tc.params)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !strings.Contains(out, "package jobs") {
				t.Errorf("missing `package jobs` in rendered body:\n%s", out)
			}
			if !strings.Contains(out, "func init()") {
				t.Errorf("missing `func init()` (no Register call?):\n%s", out)
			}
			if !strings.Contains(out, "sparkwing.Register") {
				t.Errorf("missing sparkwing.Register call:\n%s", out)
			}
			// Hard syntax check: parse the rendered body. Catches any
			// dangling brace / unbalanced if-end / mistyped identifier
			// that the substring assertions above would miss.
			fset := token.NewFileSet()
			if _, err := parser.ParseFile(fset, "rendered.go", out, parser.AllErrors); err != nil {
				t.Errorf("rendered body is not valid Go: %v\n%s", err, out)
			}
		})
	}
}

func TestListNames_StableOrder(t *testing.T) {
	a := ListNames()
	b := ListNames()
	if len(a) != len(b) {
		t.Fatalf("ListNames length drift")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("ListNames[%d]: %q vs %q", i, a[i], b[i])
		}
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
