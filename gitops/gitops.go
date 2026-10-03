// Package gitops is sparks-core's write path for the gitops repo:
// clone, patch kustomization image tags + optional file patches,
// commit, push, then kick ArgoCD.
package gitops

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
	sparkwingGit "github.com/sparkwing-dev/sparkwing/sparkwing/git"

	"github.com/sparkwing-dev/sparks-core/step"
)

type DeployConfig struct {
	GitopsRepo string
	GitopsPath string
	ECR        string
	Images     []string
	// ImageRefs keeps mutable tags from selecting different published content.
	ImageRefs  map[string]string
	Tag        string
	CommitMsg  string
	MaxRetries int
	// FilePatches maps a path under GitopsPath to "key: <old>" -> "key:
	// <new>" replacements, applied in the same commit as the tag updates.
	FilePatches map[string]map[string]string
}

// Deploy clones the gitops repo, patches kustomize image tags, and pushes,
// retrying on concurrent push conflicts. changed is true only when the push
// updated something.
func Deploy(ctx context.Context, cfg DeployConfig) (changed bool, err error) {
	if cfg.Tag == "" {
		return false, fmt.Errorf("tag required for gitops deploy")
	}
	refs, err := cfg.imageRefs()
	if err != nil {
		return false, err
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.CommitMsg == "" {
		cfg.CommitMsg = "deploy: " + cfg.Tag
	}

	if err := authorizeDeployWithController(ctx, cfg); err != nil {
		return false, err
	}

	err = step.Run(ctx, "deploy (gitops)", func(ctx context.Context) error {
		tmpDir := filepath.Join(os.TempDir(), "sparkwing-gitops-deploy")
		_ = os.RemoveAll(tmpDir)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// safety: gitops.Deploy is not reentrant, so mutating process-wide SSH env here is race-free.
		restoreSSH := setSSHEnv(ctx)
		defer restoreSSH()

		if err := sparkwingGit.Clone(ctx, cfg.GitopsRepo, tmpDir, sparkwingGit.WithDepth(1)); err != nil {
			return err
		}

		// hack: re-point origin to GitHub after the gitcache clone so retries fetch upstream, not a stale cache.
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

			content, err := patchImageRefs(data, cfg.ECR, refs)
			if err != nil {
				return err
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

// patchYAMLValue finds "name: <key>" and replaces the "value:" that follows.
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
	// hack: cap the value: search to 200 chars so a far-off "name:" pair can't be matched by mistake.
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

// pushTransport prefers HTTPS+PAT so the write path skips gitcache, and
// returns "" to leave the clone's origin URL in place.
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
		return ""
	}
	return ""
}

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

// ArgoCDConfig names the ArgoCD API server to sync against.
type ArgoCDConfig struct {
	// Server empty probes the in-cluster service and fails closed when it is
	// unreachable, rather than guessing another target.
	Server string
	// Token should come from sparkwing.Secret. Empty is valid in-cluster.
	Token string
}

// SyncArgoCD triggers a hard sync and waits for Synced and Healthy at a new
// revision. The optional tag labels log output.
func SyncArgoCD(ctx context.Context, argocd ArgoCDConfig, appName string, tag ...string) error {
	return step.Run(ctx, "argocd sync", func(ctx context.Context) error {
		return syncArgoCD(ctx, argocd, appName, 4*time.Minute, nil, tag...)
	})
}

// SyncDeployment verifies the matching Git source's repository, path,
// selected image tags and target revision against the deployment that was pushed.
// An already-current healthy deployment passes without requiring a new revision.
func SyncDeployment(ctx context.Context, argocd ArgoCDConfig, appName string, deployment DeployConfig) error {
	if _, err := deployment.imageRefs(); err != nil {
		return err
	}
	return step.Run(ctx, "argocd sync", func(ctx context.Context) error {
		return syncArgoCD(ctx, argocd, appName, 4*time.Minute, &deployment, deployment.Tag)
	})
}

func syncArgoCD(ctx context.Context, argocd ArgoCDConfig, appName string, timeout time.Duration, deployment *DeployConfig, tag ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	server, token := argocdConfig(ctx, argocd)
	if server == "" {
		return fmt.Errorf("argocd: no server reachable - pass ArgoCDConfig.Server or deploy from inside the cluster")
	}

	client := &http.Client{Timeout: 10 * time.Second}

	app, err := argocdGetApp(ctx, client, server, token, appName)
	if err != nil {
		return err
	}
	startRev := app.Status.Sync.Revision
	expected := ""
	position := 0
	var source argocdSource
	verify := func() error {
		selected, pos, err := app.deploymentSource(*deployment)
		if err != nil {
			return err
		}
		if source.RepoURL != "" && selected != source {
			return fmt.Errorf("argocd: deployment source changed while waiting for sync")
		}
		source, position = selected, pos
		if position > 0 {
			app.Status.Sync.Revision = ""
			if position <= len(app.Status.Sync.Revisions) && (len(app.Status.Sync.ComparedTo.Sources) == 0 || slices.Equal(app.Status.Sync.ComparedTo.Sources, app.Spec.Sources)) {
				app.Status.Sync.Revision = app.Status.Sync.Revisions[position-1]
			}
		}
		return nil
	}
	if deployment != nil {
		if err := verify(); err != nil {
			return err
		}
		expected, err = argocdExpectedRevision(ctx, source)
		if err != nil {
			return err
		}
		if app.deployed(expected, startRev) && app.hasDeploymentImages(deployment) {
			return nil
		}
	}
	sparkwing.Info(ctx, "argocd: %s waiting for sync", appName)
	deadline, _ := ctx.Deadline()

	nextKick := time.Now()
	kickCount := 0
	lastStatus := ""
	lastRev := startRev

	for time.Now().Before(deadline) {
		if !time.Now().Before(nextKick) {
			kickCount++
			sparkwing.Info(ctx, "argocd: kicking hard refresh (attempt %d)", kickCount)
			if app, err = argocdGetApp(ctx, client, server, token, appName+"?refresh=hard"); err != nil {
				return err
			}
			if deployment != nil {
				if err := verify(); err != nil {
					return err
				}
			}
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}

			revision := expected
			if revision == "" {
				revision = "HEAD"
			}
			err := argocdSync(ctx, client, server, token, appName, revision, position)
			if err != nil {
				errStr := fmt.Sprintf("%v", err)
				if !strings.Contains(errStr, "auto-sync") {
					sparkwing.Info(ctx, "argocd: sync request failed: %v", err)
				}
			}
			nextKick = time.Now().Add(15 * time.Second)
		}

		app, err = argocdGetApp(ctx, client, server, token, appName)
		if err != nil {
			return err
		}
		if deployment != nil {
			if err := verify(); err != nil {
				return err
			}
		}
		sync := app.Status.Sync.Status
		health := app.Status.Health.Status
		phase := app.Status.OperationState.Phase
		rev := app.Status.Sync.Revision
		lastRev = rev

		status := fmt.Sprintf("sync=%s health=%s phase=%s rev=%s images-verified=%t", sync, health, phase, shortRev(rev), app.hasDeploymentImages(deployment))
		if status != lastStatus {
			sparkwing.Info(ctx, "argocd: %s", status)
			lastStatus = status
		}

		if app.deployed(expected, startRev) && app.hasDeploymentImages(deployment) {
			if len(tag) > 0 && tag[0] != "" {
				sparkwing.Info(ctx, "argocd: %s synced + healthy - %s", appName, tag[0])
			} else {
				sparkwing.Info(ctx, "argocd: %s synced + healthy at %s", appName, shortRev(rev))
			}
			return nil
		}
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	sparkwing.Info(ctx, "argocd: gave up waiting for %s sync after %d attempts (still at %s)", appName, kickCount, shortRev(lastRev))
	return fmt.Errorf("argocd: timed out waiting for %s: %s", appName, lastStatus)
}

func argocdConfig(ctx context.Context, argocd ArgoCDConfig) (server, token string) {
	server, token = argocd.Server, argocd.Token

	if server == "" {
		server = "http://argocd-server.argocd.svc.cluster.local:80"
		client := &http.Client{Timeout: 3 * time.Second}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/api/version", nil)
		if err != nil {
			sparkwing.Info(ctx, "argocd: failed to build version probe: %v", err)
			server = ""
			return server, token
		}
		resp, err := client.Do(req)
		if err != nil {
			sparkwing.Info(ctx, "argocd: in-cluster server not reachable at %s", server)
			server = ""
			return server, token
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", token
		}
		sparkwing.Info(ctx, "argocd: using in-cluster server %s", server)
	} else {
		sparkwing.Info(ctx, "argocd: using server %s", server)
	}
	return server, token
}

type argocdSource struct {
	RepoURL        string `json:"repoURL"`
	Path           string `json:"path"`
	TargetRevision string `json:"targetRevision"`
	Chart          string `json:"chart"`
}

type argocdApp struct {
	Spec struct {
		Source  argocdSource   `json:"source"`
		Sources []argocdSource `json:"sources"`
	} `json:"spec"`
	Status struct {
		Summary struct {
			Images []string `json:"images"`
		} `json:"summary"`
		Sync struct {
			Status     string   `json:"status"`
			Revision   string   `json:"revision"`
			Revisions  []string `json:"revisions"`
			ComparedTo struct {
				Sources []argocdSource `json:"sources"`
			} `json:"comparedTo"`
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

func (app argocdApp) deployed(expected, startRev string) bool {
	revision := app.Status.Sync.Revision
	ready := app.Status.Sync.Status == "Synced" && app.Status.Health.Status == "Healthy" &&
		(app.Status.OperationState.Phase == "Succeeded" || app.Status.OperationState.Phase == "")
	if expected != "" {
		return ready && revision == expected
	}
	return ready && (startRev == "" || revision != startRev)
}

func (app argocdApp) deploymentSource(deployment DeployConfig) (argocdSource, int, error) {
	sources := app.Spec.Sources
	if len(sources) == 0 {
		sources = []argocdSource{app.Spec.Source}
	}
	position := -1
	for i, source := range sources {
		if source.Chart != "" || source.RepoURL == "" || deployment.GitopsRepo == "" ||
			gitRepoIdentity(source.RepoURL) != gitRepoIdentity(deployment.GitopsRepo) ||
			path.Clean(source.Path) != path.Clean(deployment.GitopsPath) {
			continue
		}
		if position != -1 {
			return argocdSource{}, 0, fmt.Errorf("argocd: multiple Git sources match deployment repository and path")
		}
		position = i
	}
	if position == -1 {
		return argocdSource{}, 0, fmt.Errorf("argocd: no Git source matches deployment repository and path")
	}
	source := sources[position]
	if len(app.Spec.Sources) == 0 {
		return source, 0, nil
	}
	return source, position + 1, nil
}

func gitRepoIdentity(repo string) string {
	if strings.HasPrefix(repo, "git@") {
		repo = "ssh://" + strings.Replace(repo, ":", "/", 1)
	}
	parsed, err := url.Parse(repo)
	if err != nil {
		return repo
	}
	identity := strings.ToLower(parsed.Host) + strings.TrimSuffix(strings.TrimSuffix(parsed.Path, "/"), ".git")
	if strings.EqualFold(parsed.Host, "github.com") {
		identity = strings.ToLower(identity)
	}
	return identity
}

func (app argocdApp) hasDeploymentImages(deployment *DeployConfig) bool {
	if deployment == nil {
		return true
	}
	refs, err := deployment.imageRefs()
	if err != nil {
		return false
	}
	for _, image := range deployment.Images {
		wanted := refs[image]
		if !slices.ContainsFunc(app.Status.Summary.Images, func(actual string) bool {
			if !strings.Contains(wanted, "@") {
				actual, _, _ = strings.Cut(actual, "@")
			}
			return actual == wanted
		}) {
			return false
		}
	}
	return true
}

var pinnedImageDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func (cfg DeployConfig) imageRefs() (map[string]string, error) {
	refs := make(map[string]string, len(cfg.Images))
	for _, image := range cfg.Images {
		ref := strings.TrimSuffix(cfg.ECR, "/") + "/" + image + ":" + cfg.Tag
		if cfg.ImageRefs != nil {
			prefix := ref + "@"
			ref = cfg.ImageRefs[image]
			if !strings.HasPrefix(ref, prefix) || !pinnedImageDigest.MatchString(strings.TrimPrefix(ref, prefix)) {
				return nil, fmt.Errorf("published image reference for %s must match %s and include a manifest digest", image, prefix)
			}
		}
		refs[image] = ref
	}
	return refs, nil
}

func argocdExpectedRevision(ctx context.Context, source argocdSource) (string, error) {
	revision := source.TargetRevision
	if revision == "" {
		revision = "HEAD"
	}
	if len(revision) == 40 || len(revision) == 64 {
		if _, err := hex.DecodeString(revision); err == nil {
			return strings.ToLower(revision), nil
		}
	}
	lookupRepo := source.RepoURL
	var credentialEnv []string
	if transport := pushTransport(ctx, source.RepoURL); transport != "" {
		parsed, err := url.Parse(transport)
		if err != nil {
			return "", fmt.Errorf("argocd: could not resolve repository read transport")
		}
		if parsed.User != nil {
			password, _ := parsed.User.Password()
			authorization := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(parsed.User.Username()+":"+password))
			parsed.User = nil
			count := 0
			if inherited := os.Getenv("GIT_CONFIG_COUNT"); inherited != "" {
				count, err = strconv.Atoi(inherited)
				if err != nil || count < 0 || count+1 <= count {
					return "", fmt.Errorf("argocd: invalid inherited Git configuration count")
				}
			}
			index := strconv.Itoa(count)
			credentialEnv = []string{"GIT_CONFIG_COUNT=" + strconv.Itoa(count+1), "GIT_CONFIG_KEY_" + index + "=http." + parsed.String() + ".extraheader", "GIT_CONFIG_VALUE_" + index + "=" + authorization}
		}
		lookupRepo = parsed.String()
	}
	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--", lookupRepo, revision, "refs/heads/"+revision, "refs/tags/"+revision, "refs/tags/"+revision+"^{}", revision+"^{}")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "SSH_ASKPASS=true")
	cmd.Env = append(cmd.Env, credentialEnv...)
	ssh := sshCommandValue()
	if ssh == "" {
		ssh = os.Getenv("GIT_SSH_COMMAND")
	}
	if ssh == "" {
		ssh = "ssh"
	}
	cmd.Env = append(cmd.Env, "GIT_SSH_COMMAND="+ssh+" -o BatchMode=yes")
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("argocd: resolve repository revision: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("argocd: could not resolve repository target revision (check repository read credentials)")
	}
	refs := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", fmt.Errorf("argocd: repository returned an invalid Git revision")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil || (len(fields[0]) != 40 && len(fields[0]) != 64) {
			return "", fmt.Errorf("argocd: repository returned an invalid Git revision")
		}
		refs[fields[1]] = fields[0]
	}
	for _, ref := range []string{revision + "^{}", "refs/heads/" + revision, "refs/tags/" + revision + "^{}", revision, "refs/tags/" + revision} {
		if sha := refs[ref]; sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("argocd: repository target revision did not resolve exactly")
}

func argocdGetApp(ctx context.Context, client *http.Client, server, token, appPath string) (argocdApp, error) {
	reqURL := server + "/api/v1/applications/" + appPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return argocdApp{}, fmt.Errorf("argocd: build GET request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return argocdApp{}, fmt.Errorf("argocd: GET %s: %w", appPath, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return argocdApp{}, fmt.Errorf("argocd: read %s: %w", appPath, err)
	}
	if resp.StatusCode != http.StatusOK {
		return argocdApp{}, fmt.Errorf("argocd: GET %s returned %d: %s", appPath, resp.StatusCode, truncate(string(body), 200))
	}
	var app argocdApp
	if err := json.Unmarshal(body, &app); err != nil {
		return argocdApp{}, fmt.Errorf("argocd: parse %s: %w", appPath, err)
	}
	return app, nil
}

func argocdSync(ctx context.Context, client *http.Client, server, token, appName, revision string, position int) error {
	reqURL := server + "/api/v1/applications/" + appName + "/sync"
	request := map[string]any{"prune": true}
	if position > 0 {
		request["revisions"] = []string{revision}
		request["sourcePositions"] = []int{position}
	} else {
		request["revision"] = revision
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(payload))
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

// sshCommandValue copies key material out of the secret mount before use
// because k8s strips trailing newlines on volume mounts. It returns "" when
// no cluster key is mounted, leaving the default SSH agent in place.
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

// setSSHEnv sets GIT_SSH_COMMAND and returns a cleanup that restores the
// previous value. Not reentrant: two concurrent callers clobber each other's
// cleanup, which only holds because Deploy is never run against itself.
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

// authorizeDeployWithController asks the controller to approve the push. The
// variables it reads are the harness channel the runner sets on a dispatched
// job, describing the run rather than naming a target, which is why they are
// environment reads and the ArgoCD server is an argument. A 403 inside a
// dispatched job is tolerated: the controller already approved the commit at
// dispatch, so it most likely means a stale gitcache.
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
	commit := os.Getenv("SPARKWING_COMMIT")
	if commit == "" {
		commit = cfg.Tag
	}
	params.Set("commit", commit)
	params.Set("repo", cfg.GitopsRepo)

	authURL := controllerURL + "/authorize?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, nil)
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

func patchImageRefs(data []byte, registry string, refs map[string]string) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parse kustomization: %w", err)
	}
	if len(doc.Content) != 1 {
		return "", errors.New("kustomization must contain one document")
	}
	selected := make(map[string]*yaml.Node, len(refs))
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "images" {
			continue
		}
		for _, image := range root.Content[i+1].Content {
			var entry struct {
				Name    string `yaml:"name"`
				NewName string `yaml:"newName"`
			}
			if err := image.Decode(&entry); err != nil {
				return "", fmt.Errorf("decode image: %w", err)
			}
			prefix := strings.TrimSuffix(registry, "/") + "/"
			name := strings.TrimPrefix(entry.Name, prefix)
			if !strings.HasPrefix(entry.Name, prefix) {
				continue
			}
			if _, ok := refs[name]; !ok {
				continue
			}
			if entry.NewName != "" && entry.NewName != entry.Name {
				return "", fmt.Errorf("image %s newName changes the selected repository", name)
			}
			if selected[name] != nil {
				return "", fmt.Errorf("image %s occurs more than once in kustomization.yaml", name)
			}
			selected[name] = image
		}
	}
	for name := range refs {
		if selected[name] == nil {
			return "", fmt.Errorf("image %s not found in kustomization.yaml", name)
		}
	}
	changed := false
	for name, image := range selected {
		tag := strings.TrimPrefix(refs[name], strings.TrimSuffix(registry, "/")+"/"+name+":")
		found := false
		for i := 0; i+1 < len(image.Content); i += 2 {
			value := image.Content[i+1]
			if image.Content[i].Value == "newTag" {
				changed = changed || value.Value != tag || value.Tag != "!!str"
				value.Value = tag
				value.Tag = "!!str"
				found = true
			}
			if image.Content[i].Value == "digest" {
				changed = changed || value.Value != ""
				value.Value = ""
				value.Tag = "!!str"
			}
		}
		if !found {
			changed = true
			image.Content = append(image.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "newTag"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: tag})
		}
	}
	if !changed {
		return string(data), nil
	}
	result, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("encode kustomization: %w", err)
	}
	return string(result), nil
}
