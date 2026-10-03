package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestPinnedImageRefsFailBeforeRepositoryWrite(t *testing.T) {
	good := "registry/app:desired@sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name string
		refs map[string]string
	}{
		{"missing", map[string]string{}},
		{"malformed", map[string]string{"app": "registry/app:desired@sha256:short"}},
		{"wrong-image", map[string]string{"app": strings.Replace(good, "registry/app:", "registry/other:", 1)}},
		{"wrong-tag", map[string]string{"app": strings.Replace(good, ":desired@", ":other@", 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Deploy(context.Background(), DeployConfig{GitopsRepo: "must-not-be-opened", ECR: "registry", Images: []string{"app"}, Tag: "desired", ImageRefs: tc.refs})
			if err == nil || !strings.Contains(err.Error(), "published image reference") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPinnedImageRefsPreserveCompleteImmutableImageFields(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	refs := map[string]string{"app": "registry/app:desired@" + digest, "other": "registry/other:desired@" + digest}
	for i := 0; i < 7; i++ {
		name := fmt.Sprintf("extra-%d", i)
		refs[name] = "registry/" + name + ":desired@sha256:" + fmt.Sprintf("%064x", i+1)
	}
	for _, old := range []string{"newTag: old@sha256:" + strings.Repeat("b", 64), "digest: sha256:" + strings.Repeat("b", 64) + "\n    newTag: old"} {
		source := "# keep document comment\napiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources: [deployment.yaml]\nimages:\n  - name: registry/app\n    " + old + "\n    # keep image comment\n  - name: registry/other\n    newTag: old\n"
		for i := 0; i < 7; i++ {
			source += fmt.Sprintf("  - name: registry/extra-%d\n    newTag: old\n", i)
		}
		patched, err := patchImageRefs([]byte(source), "registry", refs)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(patched, "keep document comment") || !strings.Contains(patched, "keep image comment") {
			t.Fatal("comments lost")
		}
		var result struct {
			Images []struct {
				Name   string
				NewTag string `yaml:"newTag"`
				Digest string
			}
		}
		if err := yaml.Unmarshal([]byte(patched), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Images) != len(refs) {
			t.Fatalf("selectedimagecount = %d", len(result.Images))
		}
		for _, image := range result.Images {
			name := strings.TrimPrefix(image.Name, "registry/")
			expected := strings.TrimPrefix(refs[name], image.Name+":")
			if image.NewTag != expected || image.Digest != "" {
				t.Fatalf("immutableimagefieldsmismatch: %+v", image)
			}
		}
	}
	if out, err := patchImageRefs([]byte("images:\n - name: registry/app\n   newTag: old\n"), "registry", refs); err == nil || out != "" {
		t.Fatalf("partial selected set produced output: %q, %v", out, err)
	}
}

func TestPinnedDeploymentImagesRejectSameTagDifferentDigest(t *testing.T) {
	ref := "registry/app:desired@sha256:" + strings.Repeat("a", 64)
	goal := DeployConfig{ECR: "registry", Images: []string{"app"}, Tag: "desired", ImageRefs: map[string]string{"app": ref}}
	app := argocdApp{}
	app.Status.Summary.Images = []string{strings.ReplaceAll(ref, "aaaa", "bbbb")}
	if app.hasDeploymentImages(&goal) {
		t.Fatal("different published digest accepted")
	}
	app.Status.Summary.Images = []string{ref}
	if !app.hasDeploymentImages(&goal) {
		t.Fatal("exact published reference rejected")
	}
}

func TestSyncDeploymentPinnedDigestFailsOnHealthyWrongContent(t *testing.T) {
	repo, sha := gitFixture(t)
	wanted := "registry/app:desired@sha256:" + strings.Repeat("a", 64)
	wrong := strings.ReplaceAll(wanted, "aaaa", "bbbb")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"spec": map[string]any{"source": map[string]any{"repoURL": repo, "targetRevision": "main"}}, "status": map[string]any{"sync": map[string]any{"status": "Synced", "revision": sha}, "health": map[string]any{"status": "Healthy"}, "summary": map[string]any{"images": []string{wrong}}}})
	}))
	defer server.Close()
	goal := DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"app"}, Tag: "desired", ImageRefs: map[string]string{"app": wanted}}
	if err := syncArgoCD(t.Context(), ArgoCDConfig{Server: server.URL}, "app", 100*time.Millisecond, &goal); err == nil {
		t.Fatal("healthy exactrevision withwrongpublishedcontent accepted")
	}
}

func TestImageRefWriterRejectsDifferentRepositoryAndClearsTagOnlyDigest(t *testing.T) {
	refs := map[string]string{"app": "registry/app:desired"}
	if out, err := patchImageRefs([]byte("images:\n - name: registry/app\n   newName: other/app\n   newTag: old\n"), "registry", refs); err == nil || out != "" {
		t.Fatalf("wrong repository produced output: %q, %v", out, err)
	}
	source := "images:\n - name: registry/app\n   newTag: old\n   digest: sha256:" + strings.Repeat("a", 64) + "\n"
	out, err := patchImageRefs([]byte(source), "registry", refs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "sha256:") || !strings.Contains(out, "newTag: desired") {
		t.Fatalf("tag-only update retained previous digest: %s", out)
	}
}
