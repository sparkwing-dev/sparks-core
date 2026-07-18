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

// configureDockerAuth deduplicates gcloud auth configure-docker per host,
// mirroring docker.ECRLogin's sync.Once-per-registry shape so concurrent
// pushes to the same Artifact Registry host authenticate exactly once.
var (
	configureDockerMu    sync.Mutex
	configureDockerOnces = map[string]*sync.Once{}
	configureDockerErrs  = map[string]error{}
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
	once, ok := configureDockerOnces[host]
	if !ok {
		once = &sync.Once{}
		configureDockerOnces[host] = once
	}
	configureDockerMu.Unlock()

	once.Do(func() {
		configureDockerErrs[host] = step.Run(ctx, "gcloud configure-docker ("+host+")", func(ctx context.Context) error {
			args := configureDockerArgs(host)
			if dryRun() {
				echoArgv(ctx, "gcloud", args)
				return nil
			}
			sparkwing.Info(ctx, "configuring docker auth for %s", host)
			return step.Exec(ctx, "gcloud", args...)
		})
	})
	return configureDockerErrs[host]
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
// in the resolved project and any impersonation target.
func getCredentialsArgs(cfg GKEConfig) []string {
	args := []string{"container", "clusters", "get-credentials", cfg.Cluster}
	if cfg.Location != "" {
		args = append(args, "--location", cfg.Location)
	}
	args = append(args, ProjectArgs(cfg.Project)...)
	args = append(args, ImpersonationArgs()...)
	return args
}
