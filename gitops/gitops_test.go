package gitops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSyncArgoCDFailsOnUnreadableApplication(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"unauthorized", 401, "unauthorized", "returned 401"},
		{"server-error", 503, "unavailable", "returned 503"},
		{"invalid-json", 200, "{", "parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			err := SyncArgoCD(context.Background(), ArgoCDConfig{Server: server.URL}, "app")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSyncArgoCDTimeoutFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":{"sync":{"status":"OutOfSync","revision":"old"},"health":{"status":"Degraded"}}}`))
	}))
	defer server.Close()
	err := syncArgoCD(context.Background(), ArgoCDConfig{Server: server.URL}, "app", 0, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestSyncArgoCDTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()
	err := SyncArgoCD(context.Background(), ArgoCDConfig{Server: server.URL}, "app")
	if err == nil || !strings.Contains(err.Error(), "GET app") {
		t.Fatalf("error = %v, want GET failure", err)
	}
}

func TestSyncArgoCDPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := SyncArgoCD(ctx, ArgoCDConfig{Server: "http://127.0.0.1:1"}, "app")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

func gitFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "fixture"), []byte("revision"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "fixture"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "fixture"}} {
		if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return repo, strings.TrimSpace(string(out))
}

func TestSyncDeploymentVerifiesAlreadyCurrentRevision(t *testing.T) {
	repo, sha := gitFixture(t)
	cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "tag", "-a", "v1", "-m", "fixture")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tag fixture: %v: %s", err, out)
	}
	for _, revision := range []string{"HEAD", "main", "refs/heads/main", "v1", "refs/tags/v1", sha} {
		t.Run(revision, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"targetRevision":%q}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired","registry/unselected:old"]}}}`, repo, revision, sha)
			}))
			defer server.Close()
			if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSyncDeploymentRejectsMatchingImageAtWrongRevision(t *testing.T) {
	repo, _ := gitFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":"old"},"health":{"status":"Healthy"},"summary":{"images":["registry/other:desired"]}}}`, repo)
	}))
	defer server.Close()
	if err := syncArgoCD(t.Context(), ArgoCDConfig{Server: server.URL}, "app", 100*time.Millisecond, &DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"other"}, Tag: "desired"}, "desired"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wrong revision error = %v, want deadline", err)
	}
}

func TestSyncDeploymentRefusesUnsupportedSources(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"spec":{"source":{"repoURL":"https://example.com/repo","chart":"chart"}}}`,
		`{"spec":{"source":{"repoURL":"https://example.com/repo"},"sources":[{}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: "https://example.com/repo", Tag: "desired"}); err == nil {
				t.Fatal("unsupported source passed")
			}
		})
	}
}

func TestSyncArgoCDGenericSourcesStillPoll(t *testing.T) {
	for _, spec := range []string{`{"source":{"repoURL":"https://example.com/repo","chart":"chart"}}`, `{"sources":[{"repoURL":"https://example.com/repo"}]}`} {
		t.Run(spec, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"spec":%s,"status":{"sync":{"status":"OutOfSync","revision":"old"},"health":{"status":"Degraded"}}}`, spec)
			}))
			defer server.Close()
			err := syncArgoCD(t.Context(), ArgoCDConfig{Server: server.URL}, "app", 100*time.Millisecond, nil)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("generic sync error = %v, want polling deadline", err)
			}
		})
	}
}

func TestSyncDeploymentRequestsVerifiedRevision(t *testing.T) {
	repo, sha := gitFixture(t)
	var synced atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var request struct {
				Revision string `json:"revision"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Revision != sha {
				t.Errorf("requested revision = %q, want %q", request.Revision, sha)
			}
			synced.Store(true)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		revision := "old"
		if synced.Load() {
			revision = sha
		}
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired","registry/unselected:old"]}}}`, repo, revision)
	}))
	defer server.Close()
	if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
		t.Fatal(err)
	}
	if !synced.Load() {
		t.Fatal("Argo sync was not requested")
	}
}

func TestSyncDeploymentRejectsWrongApplication(t *testing.T) {
	repo, sha := gitFixture(t)
	for _, tc := range []struct{ name, sourceRepo, sourcePath string }{
		{"wrong-repository", repo + "-other", "app"},
		{"wrong-path", repo, "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"path":%q,"targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired"]}}}`, tc.sourceRepo, tc.sourcePath, sha)
			}))
			defer server.Close()
			err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"})
			if err == nil || !strings.Contains(err.Error(), "no Git source matches") {
				t.Fatalf("wrong application error = %v", err)
			}
		})
	}
}

func TestSyncDeploymentRejectsMissingSelectedImage(t *testing.T) {
	repo, sha := gitFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/other:desired"]}}}`, repo, sha)
	}))
	defer server.Close()
	goal := DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"app"}, Tag: "desired"}
	err := syncArgoCD(t.Context(), ArgoCDConfig{Server: server.URL}, "app", 100*time.Millisecond, &goal)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing selected image error = %v", err)
	}
}

func TestDeployRejectsMissingSelectedImage(t *testing.T) {
	repo, _ := gitFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "kustomization.yaml"), []byte("images:\n  - name: registry/app\n    newTag: desired\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "kustomization.yaml"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "images"}} {
		if out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v: %s", err, out)
		}
	}
	_, err := Deploy(t.Context(), DeployConfig{GitopsRepo: repo, ECR: "registry", Images: []string{"app", "missing"}, Tag: "desired"})
	if err == nil || !strings.Contains(err.Error(), "image missing not found") {
		t.Fatalf("missing image error = %v", err)
	}
}

func TestDeploymentImagesAcceptExactTagsWithDigestPins(t *testing.T) {
	for _, tc := range []struct {
		name, tag, actual string
		want              bool
	}{
		{"pinned", "desired", "registry/app:desired@sha256:abc", true},
		{"wrong-tag", "desired", "registry/app:desired-evil@sha256:abc", false},
		{"wrong-image", "desired", "registry/other:desired@sha256:abc", false},
		{"exact-digest", "desired@sha256:abc", "registry/app:desired@sha256:abc", true},
		{"wrong-digest", "desired@sha256:abc", "registry/app:desired@sha256:def", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var app argocdApp
			app.Status.Summary.Images = []string{tc.actual, "registry/unselected:old"}
			goal := DeployConfig{ECR: "registry", Images: []string{"app"}, Tag: tc.tag}
			if got := app.hasDeploymentImages(&goal); got != tc.want {
				t.Fatalf("image matched = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestSyncDeploymentMultiSourceAlreadyCurrent(t *testing.T) {
	repo, sha := gitFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":"ignored","path":"ignored"},"sources":[{"repoURL":"https://example.com/shared","path":"config"},{"repoURL":%q,"path":"app","targetRevision":%q}]},"status":{"sync":{"status":"Synced","revisions":["shared",%q]},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired"]}}}`, repo, sha, sha)
	}))
	defer server.Close()
	if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
		t.Fatal(err)
	}
}

func TestSyncDeploymentMultiSourceMatchRequired(t *testing.T) {
	for _, tc := range []struct{ name, sources, want string }{
		{"missing", `[{"repoURL":"https://example.com/other","path":"app"}]`, "no Git source"},
		{"ambiguous", `[{"repoURL":"https://example.com/repo","path":"app"},{"repoURL":"https://example.com/repo.git","path":"app"}]`, "multiple Git sources"},
		{"helm", `[{"repoURL":"https://example.com/repo","path":"app","chart":"chart"}]`, "no Git source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":"https://example.com/repo","path":"app"},"sources":%s}}`, tc.sources)
			}))
			defer server.Close()
			err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: "https://example.com/repo", GitopsPath: "app", Tag: "desired"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestSyncDeploymentMultiSourceRejectsStaleStatus(t *testing.T) {
	repo, sha := gitFixture(t)
	for _, tc := range []struct{ name, revision, image string }{
		{"revision", "stale", "desired"}, {"image", sha, "stale"}, {"compared-source-order", sha, "desired"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				compared := ""
				if tc.name == "compared-source-order" {
					compared = fmt.Sprintf(`,"comparedTo":{"sources":[{"repoURL":%q,"path":"app","targetRevision":%q},{"repoURL":"https://example.com/shared"}]}`, repo, sha)
				}
				_, _ = fmt.Fprintf(w, `{"spec":{"sources":[{"repoURL":"https://example.com/shared"},{"repoURL":%q,"path":"app","targetRevision":%q}]},"status":{"sync":{"status":"Synced","revisions":[%q,%q]%s},"health":{"status":"Healthy"},"summary":{"images":["registry/app:%s"]}}}`, repo, sha, sha, tc.revision, compared, tc.image)
			}))
			defer server.Close()
			err := syncArgoCD(t.Context(), ArgoCDConfig{Server: server.URL}, "app", 100*time.Millisecond, &DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v, want deadline", err)
			}
		})
	}
}

func TestSyncDeploymentMultiSourceReordersAndRequestsExactRevision(t *testing.T) {
	repo, sha := gitFixture(t)
	var synced atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var request struct {
				Revision        string
				Revisions       []string
				SourcePositions []int
				Prune           bool
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Revision != "" || len(request.Revisions) != 1 || request.Revisions[0] != sha || len(request.SourcePositions) != 1 || request.SourcePositions[0] != 1 || !request.Prune {
				t.Errorf("sync request = %+v", request)
			}
			synced.Store(true)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		selected := fmt.Sprintf(`{"repoURL":%q,"path":"app","targetRevision":%q}`, repo, sha)
		other := `{"repoURL":"https://example.com/shared","path":"config"}`
		sources := other + "," + selected
		revisions := `"shared","old"`
		if r.URL.RawQuery != "" {
			sources = selected + "," + other
			revisions = `"old","shared"`
		}
		if synced.Load() {
			revisions = fmt.Sprintf(`"shared",%q`, sha)
		}
		_, _ = fmt.Fprintf(w, `{"spec":{"sources":[%s]},"status":{"sync":{"status":"Synced","revisions":[%s]},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired"]}}}`, sources, revisions)
	}))
	defer server.Close()
	if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
		t.Fatal(err)
	}
	if !synced.Load() {
		t.Fatal("no indexed sync request")
	}
}

func TestSyncDeploymentPATOnlySSHSource(t *testing.T) {
	const repo = "git@github.com:owner/private.git"
	const token = "fixture-pat-secret"
	const sha = "0123456789012345678901234567890123456789"
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("GIT_CONFIG_COUNT", "0")
	authorization := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in *fixture-pat-secret*) exit 4;; esac\n[ \"$4\" = 'https://github.com/owner/private.git' ] || exit 1\n[ \"$GIT_CONFIG_KEY_0\" = 'http.https://github.com/owner/private.git.extraheader' ] || exit 2\n[ \"$GIT_CONFIG_VALUE_0\" = '%s' ] || exit 3\nprintf '%s refs/heads/main\\n'\n", authorization, sha)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GITHUB_TOKEN", token)
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /missing-key")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"spec":{"source":{"repoURL":%q,"path":"app","targetRevision":"main"}},"status":{"sync":{"status":"Synced","revision":%q},"health":{"status":"Healthy"},"summary":{"images":["registry/app:desired"]}}}`, repo, sha)
	}))
	defer server.Close()
	if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "http.proxy")
	t.Setenv("GIT_CONFIG_VALUE_0", "http://proxy.invalid:8080")
	t.Setenv("GIT_CONFIG_KEY_1", "http.sslCAInfo")
	t.Setenv("GIT_CONFIG_VALUE_1", "/fixture/custom-ca.pem")
	inheritedScript := fmt.Sprintf("#!/bin/sh\n[ \"$GIT_CONFIG_COUNT\" = 3 ] || exit 1\n[ \"$(%q config --get http.proxy)\" = 'http://proxy.invalid:8080' ] || exit 2\n[ \"$(%q config --get http.sslCAInfo)\" = '/fixture/custom-ca.pem' ] || exit 3\n[ \"$(%q config --get-urlmatch http.extraheader https://github.com/owner/private.git)\" = '%s' ] || exit 4\nprintf '%s refs/heads/main\\n'\n", realGit, realGit, realGit, authorization, sha)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(inheritedScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho fixture-pat-secret >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", ECR: "registry", Images: []string{"app"}, Tag: "desired"})
	if err == nil || strings.Contains(err.Error(), token) || !strings.Contains(err.Error(), "check repository read credentials") {
		t.Fatalf("redacted lookup error = %v", err)
	}
	for _, count := range []string{"invalid", "-1", "999999999999999999999999999999"} {
		t.Setenv("GIT_CONFIG_COUNT", count)
		err := SyncDeployment(t.Context(), ArgoCDConfig{Server: server.URL}, "app", DeployConfig{GitopsRepo: repo, GitopsPath: "app", Tag: "desired"})
		if err == nil || !strings.Contains(err.Error(), "invalid inherited Git configuration count") {
			t.Fatalf("count %s error = %v", count, err)
		}
	}
}
