package gcp

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// dryRun reports whether SPARKWING_DRY_RUN is active.
func dryRun() bool {
	return os.Getenv("SPARKWING_DRY_RUN") != ""
}

// echoArgv logs the exact command that would run under SPARKWING_DRY_RUN.
// Callers return nil after this so a cloud-mutating step is a no-op that
// still shows its argv in the log stream.
func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

// dockerAuthState memoizes one gcloud configure-docker per Artifact
// Registry host: once runs the configuration a single time and err holds
// its result. err is written inside once.Do and read only after it
// returns, so once's happens-before makes it safe without its own lock.
type dockerAuthState struct {
	once sync.Once
	err  error
}

// configureDockerHosts hands out one dockerAuthState per host. Only the
// map itself is guarded by configureDockerMu; the per-host state is
// synchronized by its own sync.Once.
var (
	configureDockerMu    sync.Mutex
	configureDockerHosts = map[string]*dockerAuthState{}
)

// ConfigureDockerAuth registers gcloud as a docker credential helper for
// an Artifact Registry host (e.g. "us-west1-docker.pkg.dev") so
// subsequent `docker push`es to that host authenticate via the active
// gcloud identity. It is the GCP twin of docker.ECRLogin.
//
// Safe to call repeatedly and concurrently: each host is configured at
// most once. Under SPARKWING_DRY_RUN the gcloud argv is echoed and no
// docker config is written.
func ConfigureDockerAuth(ctx context.Context, host string) error {
	configureDockerMu.Lock()
	state, ok := configureDockerHosts[host]
	if !ok {
		state = &dockerAuthState{}
		configureDockerHosts[host] = state
	}
	configureDockerMu.Unlock()

	state.once.Do(func() {
		state.err = step.Run(ctx, "gcloud configure-docker ("+host+")", func(ctx context.Context) error {
			args := configureDockerArgs(host)
			if dryRun() {
				echoArgv(ctx, "gcloud", args)
				return nil
			}
			sparkwing.Info(ctx, "configuring docker auth for %s", host)
			return step.Exec(ctx, "gcloud", args...)
		})
	})
	return state.err
}

// configureDockerArgs is the gcloud argv ConfigureDockerAuth runs.
func configureDockerArgs(host string) []string {
	return []string{"auth", "configure-docker", host, "--quiet"}
}

// GKEConfig identifies a GKE cluster for GetGKECredentials. Location is
// the cluster's region or zone (e.g. "us-west1" or "us-west1-a").
type GKEConfig struct {
	Cluster  string
	Location string
	Project  string
	// ExtraArgs are appended verbatim to the get-credentials argv, an
	// escape hatch for flags this struct does not model. Private control
	// planes need ExtraArgs: []string{"--internal-ip"} (or
	// []string{"--dns-endpoint"}); other get-credentials flags fit here too.
	ExtraArgs []string
}

// GetGKECredentials fetches GKE cluster credentials with
// `gcloud container clusters get-credentials`, writing a kubeconfig
// context the kube block then targets. The command reaches the GKE
// control plane for the cluster endpoint and CA, so under SPARKWING_DRY_RUN
// the argv is echoed and no kubeconfig is written.
func GetGKECredentials(ctx context.Context, cfg GKEConfig) error {
	return step.Run(ctx, "gcloud get-credentials ("+cfg.Cluster+")", func(ctx context.Context) error {
		args := getCredentialsArgs(cfg)
		if dryRun() {
			echoArgv(ctx, "gcloud", args)
			return nil
		}
		sparkwing.Info(ctx, "fetching GKE credentials for %s", cfg.Cluster)
		return step.Exec(ctx, "gcloud", args...)
	})
}

// getCredentialsArgs is the gcloud argv GetGKECredentials runs, folding
// in the resolved project, any caller ExtraArgs, and any impersonation
// target.
func getCredentialsArgs(cfg GKEConfig) []string {
	args := []string{"container", "clusters", "get-credentials", cfg.Cluster}
	if cfg.Location != "" {
		args = append(args, "--location", cfg.Location)
	}
	args = append(args, ProjectArgs(cfg.Project)...)
	args = append(args, cfg.ExtraArgs...)
	args = append(args, ImpersonationArgs()...)
	return args
}
