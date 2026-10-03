package deploy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sparkwing-dev/sparks-core/gitops"
)

func TestRunVerifiesUnchangedTags(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "kustomization.yaml"), []byte("images:\n  - name: registry/app\n    newTag: desired\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "kustomization.yaml"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture"}} {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	var observed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed.Add(1)
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"targetRevision":"HEAD"}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired"]}}}`, repo, sha)
	}))
	defer server.Close()
	if err := Run(context.Background(), Config{GitopsRepo: repo, GitopsPath: ".", ECR: "registry", Images: []string{"app"}, Tag: "desired", AppName: "app", ArgoCD: gitops.ArgoCDConfig{Server: server.URL}}); err != nil {
		t.Fatal(err)
	}
	if observed.Load() == 0 {
		t.Fatal("unchanged tags bypassed ArgoCD verification")
	}
}
