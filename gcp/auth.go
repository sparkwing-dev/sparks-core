package gcp

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

func dryRun() bool {
	return os.Getenv("SPARKWING_DRY_RUN") != ""
}

func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

// dockerAuthState.err is written inside once.Do and read only after it
// returns, so once's happens-before makes it safe without its own lock.
type dockerAuthState struct {
	once sync.Once
	err  error
}

// configureDockerMu guards the map only; each host's state is synchronized
// by its own sync.Once.
var (
	configureDockerMu    sync.Mutex
	configureDockerHosts = map[string]*dockerAuthState{}
)

// ConfigureDockerAuth registers gcloud as a docker credential helper for an
// Artifact Registry host. It is safe to call repeatedly and concurrently:
// each host is configured at most once.
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

func configureDockerArgs(host string) []string {
	return []string{"auth", "configure-docker", host, "--quiet"}
}

// GKEConfig identifies a GKE cluster for GetGKECredentials. Location is the
// cluster's region or zone.
type GKEConfig struct {
	Cluster                   string
	Location                  string
	Project                   string
	ImpersonateServiceAccount string
	// ExtraArgs are appended verbatim to the get-credentials argv. Private
	// control planes need --internal-ip or --dns-endpoint here.
	ExtraArgs []string
}

// GetGKECredentials writes a kubeconfig context the kube block then targets.
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

func getCredentialsArgs(cfg GKEConfig) []string {
	args := []string{"container", "clusters", "get-credentials", cfg.Cluster}
	if cfg.Location != "" {
		args = append(args, "--location", cfg.Location)
	}
	args = append(args, ProjectArgs(cfg.Project)...)
	args = append(args, cfg.ExtraArgs...)
	args = append(args, ImpersonationArgs(cfg.ImpersonateServiceAccount)...)
	return args
}
