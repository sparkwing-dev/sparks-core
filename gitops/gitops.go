// Package gitops is sparks-core's write path for the gitops repo:
// clone, patch kustomization image tags + optional file patches,
// commit, push, then kick ArgoCD.
package gitops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
	sparkwingGit "github.com/sparkwing-dev/sparkwing/sparkwing/git"

	"github.com/sparkwing-dev/sparks-core/step"
)

// DeployConfig configures a gitops deployment.
type DeployConfig struct {
	GitopsRepo string
	GitopsPath string
	ECR        string
	Images     []string
	Tag        string
	CommitMsg  string
	MaxRetries int
	// FilePatches is a map of relative file paths (within GitopsPath)
	// to key-value replacements. Each entry patches "key: <old>" to
	// "key: <new>" in the specified file, in the same commit as the
	// image tag updates. Used to keep deployment env vars in sync
	// with image tags.
	FilePatches map[string]map[string]string
}

// Deploy clones the gitops repo, patches kustomize image tags, and
// pushes. Returns (changed, err) -- changed is true iff the push
// actually updated anything.
//
// Clone is via gitcache when reachable (fast read cache); push is
// direct to GitHub via GITHUB_TOKEN PAT, falling back to SSH.
// Retries on concurrent push conflicts.
func Deploy(ctx context.Context, cfg DeployConfig) (changed bool, err error) {
	if cfg.Tag == "" {
		return false, fmt.Errorf("tag required for gitops deploy")
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.CommitMsg == "" {
		cfg.CommitMsg = "deploy: " + cfg.Tag
	}

	// Pre-deploy authorization: phone home to the controller if one
	// is configured. Logs the request for audit and (for scoped
	// tokens) enforces environment restrictions. Skipped with
	// SPARKWING_NO_VERIFY=1 as a break-glass.
	if err := authorizeDeployWithController(ctx, cfg); err != nil {
		return false, err
	}

	err = step.Run(ctx, "deploy (gitops)", func(ctx context.Context) error {
		tmpDir := filepath.Join(os.TempDir(), "sparkwing-gitops-deploy")
		_ = os.RemoveAll(tmpDir)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Set GIT_SSH_COMMAND for this Deploy invocation. Clone routes
		// through gitcache when available; the SSH key is needed for the
		// SSH fallback path and for the final push when no GITHUB_TOKEN
		// PAT is present. Process-env set is safe here: gitops.Deploy is
		// not reentrant against itself.
		restoreSSH := setSSHEnv(ctx)
		defer restoreSSH()

		if err := sparkwingGit.Clone(ctx, cfg.GitopsRepo, tmpDir, sparkwingGit.WithDepth(1)); err != nil {
			return err
		}

		// Set origin to GitHub (both fetch and push) so retries fetch
		// from the real upstream, not from gitcache which may be a
		// few seconds behind. The initial clone from gitcache is
		// purely for speed.
		if pushRemote := pushTransport(ctx, cfg.GitopsRepo); pushRemote != "" {
			if err := step.Exec(ctx, "git", "-C", tmpDir, "remote", "set-url", "origin", pushRemote); err != nil {
				return err
			}
		}

		kustomizePath := filepath.Join(tmpDir, cfg.GitopsPath, "kustomization.yaml")

		for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
			if attempt > 1 {
				sparkwing.Info(ctx, "push failed (attempt %d/%d), pulling and retrying...", attempt-1, cfg.MaxRetries)
				if err := step.Exec(ctx, "git", "-C", tmpDir, "fetch", "origin", "main"); err != nil {
					return err
				}
				if err := step.Exec(ctx, "git", "-C", tmpDir, "reset", "--hard", "origin/main"); err != nil {
					return err
				}
			}

			data, err := os.ReadFile(kustomizePath)
			if err != nil {
				return fmt.Errorf("read kustomization.yaml: %w", err)
			}

			content := string(data)
			for _, img := range cfg.Images {
				ecr := cfg.ECR + "/" + img
				old := fmt.Sprintf("name: %s\n    newTag: ", ecr)
				idx := strings.Index(content, old)
				if idx == -1 {
					sparkwing.Info(ctx, "warning: image %s not found in kustomization.yaml, skipping", img)
					continue
				}
				after := idx + len(old)
				eol := strings.Index(content[after:], "\n")
				if eol == -1 {
					eol = len(content[after:])
				}
				content = content[:after] + cfg.Tag + content[after+eol:]
				sparkwing.Info(ctx, "  %s -> %s", img, cfg.Tag)
			}

			if err := os.WriteFile(kustomizePath, []byte(content), 0o644); err != nil {
				return fmt.Errorf("write kustomization.yaml: %w", err)
			}

			for relPath, patches := range cfg.FilePatches {
				patchPath := filepath.Join(tmpDir, cfg.GitopsPath, relPath)
				pData, err := os.ReadFile(patchPath)
				if err != nil {
					sparkwing.Info(ctx, "warning: file patch %s: %v", relPath, err)
					continue
				}
				pContent := string(pData)
				for key, value := range patches {
					pContent = patchYAMLValue(ctx, pContent, key, value)
				}
				if err := os.WriteFile(patchPath, []byte(pContent), 0o644); err != nil {
					sparkwing.Info(ctx, "warning: file patch write %s: %v", relPath, err)
				}
			}

			if err := step.Exec(ctx, "git", "-C", tmpDir, "add", "-A"); err != nil {
				return err
			}
			if _, noChanges := sparkwing.Exec(ctx, "git", "-C", tmpDir, "diff", "--cached", "--quiet").Run(); noChanges == nil {
				sparkwing.Info(ctx, "tags already up to date - nothing to push")
				return nil
			}

			changed = true
			if err := step.Exec(
				ctx, "git", "-C", tmpDir,
				"-c", "user.name=sparkwing",
				"-c", "user.email=sparkwing@noreply",
				"commit", "-m", cfg.CommitMsg,
			); err != nil {
				return err
			}

			_, pushErr := sparkwing.Exec(ctx, "git", "-C", tmpDir, "push").Run()
			if pushErr == nil {
				return nil
			}
		}
		return fmt.Errorf("gitops push failed after %d attempts", cfg.MaxRetries)
	})
	return changed, err
}

// patchYAMLValue finds "name: <key>" in a k8s YAML file and replaces
// the "value:" on the following line. Handles the common env var
// pattern:
//
//   - name: SPARKWING_RUNNER_IMAGE
//     value: old-image:old-tag
func patchYAMLValue(ctx context.Context, content, key, newValue string) string {
	nameNeedle := "name: " + key
	nameIdx := strings.Index(content, nameNeedle)
	if nameIdx == -1 {
		sparkwing.Info(ctx, "warning: patch key %q not found in file", key)
		return content
	}

	afterName := nameIdx + len(nameNeedle)
	rest := content[afterName:]
	valueNeedle := "value: "
	valueIdx := strings.Index(rest, valueNeedle)
	// value: should be on the very next line, not 200 chars away --
	// guards against matching the wrong pair in files where multiple
	// "name: X" keys sit near each other.
	if valueIdx == -1 || valueIdx > 200 {
		sparkwing.Info(ctx, "warning: no value: found after name: %s", key)
		return content
	}

	absValueIdx := afterName + valueIdx + len(valueNeedle)
	eol := strings.Index(content[absValueIdx:], "\n")
	if eol == -1 {
		eol = len(content[absValueIdx:])
	}
	old := strings.TrimSpace(content[absValueIdx : absValueIdx+eol])
	content = content[:absValueIdx] + newValue + content[absValueIdx+eol:]
	sparkwing.Info(ctx, "  patched %s: %s -> %s", key, old, newValue)
	return content
}

// pushTransport returns the push URL for the gitops repo. Prefers
// GitHub HTTPS+PAT (direct, no gitcache in write path), falls back to
// SSH. Returns "" when neither is available (origin URL from the clone
// is used as-is). SSH key setup is handled by setSSHEnv in the caller;
// this function only resolves the remote URL.
func pushTransport(ctx context.Context, sshURL string) string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		httpsURL := sshToHTTPS(sshURL, token)
		if httpsURL != "" {
			sparkwing.Info(ctx, "gitops push via HTTPS+PAT")
			return httpsURL
		}
	}
	if sshCommandValue() != "" {
		sparkwing.Info(ctx, "gitops push via SSH")
		return "" // keep origin as cloned (SSH URL)
	}
	return ""
}

// sshToHTTPS converts "git@github.com:owner/repo.git" to
// "https://x-access-token:<token>@github.com/owner/repo.git".
func sshToHTTPS(sshURL, token string) string {
	if !strings.HasPrefix(sshURL, "git@") {
		return ""
	}
	rest := strings.TrimPrefix(sshURL, "git@")
	idx := strings.Index(rest, ":")
	if idx < 0 {
		return ""
	}
	host := rest[:idx]
	path := rest[idx+1:]
	return fmt.Sprintf("https://x-access-token:%s@%s/%s", token, host, path)
}

// SyncArgoCD triggers a hard ArgoCD sync for the named application
// and waits until the synced revision advances past the starting
// point and reports Synced + Healthy.
//
// Uses the ArgoCD REST API (SPARKWING_ARGOCD_SERVER +
// SPARKWING_ARGOCD_TOKEN) so it works from anywhere. Falls back to
// in-cluster service discovery if the env vars are not set.
func SyncArgoCD(ctx context.Context, appName string, tag ...string) error {
	return step.Run(ctx, "argocd sync", func(ctx context.Context) error {
		server, token := argocdConfig(ctx)
		if server == "" {
			return fmt.Errorf("argocd: no server reachable - set SPARKWING_ARGOCD_SERVER or deploy from inside the cluster")
		}

		client := &http.Client{Timeout: 10 * time.Second}

		app := argocdGetApp(ctx, client, server, token, appName)
		startRev := app.Status.Sync.Revision
		sparkwing.Info(ctx, "argocd: %s currently at %s - kicking refresh", appName, shortRev(startRev))

		deadline := time.Now().Add(4 * time.Minute)
		nextKick := time.Now()
		kickCount := 0
		lastStatus := ""

		for time.Now().Before(deadline) {
			if !time.Now().Before(nextKick) {
				kickCount++
				sparkwing.Info(ctx, "argocd: kicking hard refresh (attempt %d)", kickCount)
				argocdGetApp(ctx, client, server, token, appName+"?refresh=hard")
				time.Sleep(2 * time.Second)

				err := argocdSync(client, server, token, appName)
				if err != nil {
					errStr := fmt.Sprintf("%v", err)
					if !strings.Contains(errStr, "auto-sync") {
						sparkwing.Info(ctx, "argocd: sync request failed: %v", err)
					}
				}
				nextKick = time.Now().Add(15 * time.Second)
			}

			app = argocdGetApp(ctx, client, server, token, appName)
			sync := app.Status.Sync.Status
			health := app.Status.Health.Status
			phase := app.Status.OperationState.Phase
			rev := app.Status.Sync.Revision

			status := fmt.Sprintf("sync=%s health=%s phase=%s rev=%s", sync, health, phase, shortRev(rev))
			if status != lastStatus {
				sparkwing.Info(ctx, "argocd: %s", status)
				lastStatus = status
			}

			advanced := startRev == "" || rev != startRev
			if sync == "Synced" && health == "Healthy" && (phase == "Succeeded" || phase == "") && advanced {
				if len(tag) > 0 && tag[0] != "" {
					sparkwing.Info(ctx, "argocd: %s synced + healthy - %s", appName, tag[0])
				} else {
					sparkwing.Info(ctx, "argocd: %s synced + healthy at %s", appName, shortRev(rev))
				}
				return nil
			}
			time.Sleep(2 * time.Second)
		}

		sparkwing.Info(ctx, "argocd: gave up waiting for %s sync after %d attempts (still at %s)", appName, kickCount, shortRev(startRev))
		sparkwing.Info(ctx, "argocd: last status: %s", lastStatus)
		return nil
	})
}

func argocdConfig(ctx context.Context) (server, token string) {
	server = os.Getenv("SPARKWING_ARGOCD_SERVER")
	token = os.Getenv("SPARKWING_ARGOCD_TOKEN")

	if server == "" {
		// In-cluster: ArgoCD server is reachable via k8s service. The
		// server runs in insecure mode (HTTP).
		server = "http://argocd-server.argocd.svc.cluster.local:80"
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(server + "/api/version")
		if err != nil || resp.StatusCode != http.StatusOK {
			sparkwing.Info(ctx, "argocd: in-cluster server not reachable at %s", server)
			server = ""
			return server, token
		}
		resp.Body.Close()
		sparkwing.Info(ctx, "argocd: using in-cluster server %s", server)
	} else {
		sparkwing.Info(ctx, "argocd: using server %s", server)
	}
	return server, token
}

type argocdApp struct {
	Status struct {
		Sync struct {
			Status   string `json:"status"`
			Revision string `json:"revision"`
		} `json:"sync"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		OperationState struct {
			Phase   string `json:"phase"`
			Message string `json:"message"`
		} `json:"operationState"`
	} `json:"status"`
}

func argocdGetApp(ctx context.Context, client *http.Client, server, token, appPath string) argocdApp {
	reqURL := server + "/api/v1/applications/" + appPath
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		sparkwing.Info(ctx, "argocd: failed to build request: %v", err)
		return argocdApp{}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		sparkwing.Info(ctx, "argocd: GET %s failed: %v", appPath, err)
		return argocdApp{}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		sparkwing.Info(ctx, "argocd: GET %s returned %d: %s", appPath, resp.StatusCode, truncate(string(body), 200))
		return argocdApp{}
	}

	var app argocdApp
	if err := json.Unmarshal(body, &app); err != nil {
		sparkwing.Info(ctx, "argocd: failed to parse response: %v", err)
	}
	return app
}

func argocdSync(client *http.Client, server, token, appName string) error {
	reqURL := server + "/api/v1/applications/" + appName + "/sync"
	payload := []byte(`{"revision":"HEAD","prune":true}`)
	req, err := http.NewRequest(http.MethodPost, reqURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST sync: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sync returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func shortRev(r string) string {
	if len(r) > 8 {
		return r[:8]
	}
	return r
}

// sshCommandValue returns the value for the GIT_SSH_COMMAND environment
// variable when cluster SSH key material is present, or "" when the
// default SSH agent should be used. In k8s pods, copies key material
// from the secret mount to /tmp (k8s strips trailing newlines on
// volume mounts). Locally, returns "" -- the default agent is assumed.
func sshCommandValue() string {
	if _, err := os.Stat("/etc/ssh-key/id_ed25519"); err == nil {
		sshDir := "/tmp/ssh-keys"
		if err := os.MkdirAll(sshDir, 0o700); err != nil {
			return ""
		}
		for _, name := range []string{"id_ed25519", "known_hosts"} {
			data, err := os.ReadFile("/etc/ssh-key/" + name)
			if err != nil {
				continue
			}
			if len(data) > 0 && data[len(data)-1] != '\n' {
				data = append(data, '\n')
			}
			if err := os.WriteFile(sshDir+"/"+name, data, 0o600); err != nil {
				return ""
			}
		}
		return fmt.Sprintf("ssh -i %s/id_ed25519 -o UserKnownHostsFile=%s/known_hosts -o StrictHostKeyChecking=yes", sshDir, sshDir)
	}
	return ""
}

// setSSHEnv writes GIT_SSH_COMMAND into the process environment when
// cluster SSH key material is present and returns a cleanup function
// that restores the previous value. Call with defer:
//
//	restore := setSSHEnv(ctx)
//	defer restore()
//
// Not reentrant: two concurrent callers would clobber each other's
// cleanup. gitops.Deploy is not called concurrently against itself
// today so this is safe; use a per-command Env option if that changes.
func setSSHEnv(ctx context.Context) func() {
	val := sshCommandValue()
	if val == "" {
		return func() {}
	}
	prev, hadPrev := os.LookupEnv("GIT_SSH_COMMAND")
	if err := os.Setenv("GIT_SSH_COMMAND", val); err != nil {
		sparkwing.Info(ctx, "gitops: warning: could not set GIT_SSH_COMMAND: %v", err)
		return func() {}
	}
	return func() {
		if hadPrev {
			_ = os.Setenv("GIT_SSH_COMMAND", prev)
		} else {
			_ = os.Unsetenv("GIT_SSH_COMMAND")
		}
	}
}

// authorizeDeployWithController calls the controller's /authorize
// endpoint before pushing to the gitops repo. The controller logs
// the request for audit and verifies the commit is on the protected
// branch.
//
// Behavior:
//   - SPARKWING_NO_VERIFY=1: skip entirely, print warning (break-glass)
//   - SPARKWING_CONTROLLER unset: skip silently (no controller)
//   - Controller unreachable: warn but continue
//   - Controller returns 403: error (unless we're inside a dispatched
//     job; that means the controller already approved the commit at
//     dispatch time and a 403 here likely means a stale gitcache).
func authorizeDeployWithController(ctx context.Context, cfg DeployConfig) error {
	if os.Getenv("SPARKWING_NO_VERIFY") == "1" {
		sparkwing.Info(ctx, "warning: --no-verify set - skipping deploy authorization")
		return nil
	}

	controllerURL := os.Getenv("SPARKWING_CONTROLLER")
	if controllerURL == "" {
		return nil
	}

	token := os.Getenv("SPARKWING_API_TOKEN")

	params := url.Values{}
	params.Set("pipeline", os.Getenv("SPARKWING_PIPELINE"))
	// Send the git commit SHA for branch verification, not the image
	// tag. The image tag contains content hashes that aren't git
	// commits.
	commit := os.Getenv("SPARKWING_COMMIT")
	if commit == "" {
		commit = cfg.Tag
	}
	params.Set("commit", commit)
	params.Set("repo", cfg.GitopsRepo)

	authURL := controllerURL + "/authorize?" + params.Encode()

	req, err := http.NewRequest(http.MethodPost, authURL, nil)
	if err != nil {
		sparkwing.Info(ctx, "authorize: failed to build request: %v (continuing)", err)
		return nil
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		sparkwing.Info(ctx, "authorize: controller unreachable (%v) - continuing without verification", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		sparkwing.Info(ctx, "authorize: approved by controller")
		return nil
	}

	if resp.StatusCode == http.StatusForbidden {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		msg := strings.TrimSpace(string(body[:n]))
		if os.Getenv("SPARKWING_JOB_ID") != "" {
			sparkwing.Info(ctx, "authorize: controller denied (%s) - continuing (job was already dispatched)", msg)
			return nil
		}
		return fmt.Errorf("deploy blocked by controller: %s", msg)
	}

	sparkwing.Info(ctx, "authorize: controller returned %d - continuing", resp.StatusCode)
	return nil
}
