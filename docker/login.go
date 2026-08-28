package docker

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
	"github.com/sparkwing-dev/sparks-core/step"
)

const dryRunEnv = "SPARKWING_DRY_RUN"

// defaultGHCRUsername is a placeholder: a classic GitHub token
// authenticates regardless of username. A fine-grained token needs the
// owning account in LoginConfig.Username.
const defaultGHCRUsername = "x-access-token"

func dryRun() bool {
	return os.Getenv(dryRunEnv) != ""
}

func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

func registryHost(registry string) string {
	if i := strings.IndexByte(registry, '/'); i >= 0 {
		return registry[:i]
	}
	return registry
}

type RegistryKind string

const (
	// RegistryECR authenticates via `aws ecr get-login-password`.
	RegistryECR RegistryKind = "ecr"
	// RegistryGAR registers gcloud as a docker credential helper.
	RegistryGAR RegistryKind = "gar"
	// RegistryGHCR pipes a token into `docker login`.
	RegistryGHCR RegistryKind = "ghcr"
)

type LoginConfig struct {
	// Kind defaults to RegistryECR.
	Kind RegistryKind
	// Registry is the host or host/prefix to authenticate with. Required.
	Registry string
	// AWSProfile empty resolves via AWS_PROFILE or drops under IRSA. ECR only.
	AWSProfile string
	// TokenSecret names the sparkwing secret holding the token. Required for
	// ghcr, which is also the only kind that reads Username.
	TokenSecret string
	// Username defaults to defaultGHCRUsername.
	Username string
}

// RegistryLogin authenticates the local docker client with a container
// registry, dispatching on LoginConfig.Kind.
func RegistryLogin(ctx context.Context, cfg LoginConfig) error {
	switch cfg.Kind {
	case RegistryECR, "":
		return ecrLogin(ctx, cfg.Registry, cfg.AWSProfile)
	case RegistryGAR:
		return garLogin(ctx, cfg.Registry)
	case RegistryGHCR:
		return ghcrLogin(ctx, cfg)
	default:
		return fmt.Errorf("docker.RegistryLogin: unknown registry kind %q (want ecr, gar, or ghcr)", cfg.Kind)
	}
}

func ecrLogin(ctx context.Context, registry, awsProfile string) error {
	return step.Run(ctx, "ecr login", func(ctx context.Context) error {
		region := ECRRegion(registry)
		profileFlag := aws.ProfileFlag(awsProfile)
		if dryRun() {
			sparkwing.Info(ctx, "DRY RUN: aws ecr get-login-password --region %s%s | docker login --username AWS --password-stdin %s",
				region, profileFlag, registry)
			return nil
		}
		sparkwing.Info(ctx, "authenticating with ECR (region=%s)", region)
		// hack: PROFILE_FLAG is intentionally unquoted so its
		// " --profile <name>" expansion word-splits into two argv tokens
		// (or vanishes when empty); the pipe needs a real shell.
		if _, err := sparkwing.Bash(
			ctx,
			`aws ecr get-login-password --region "$REGION"${PROFILE_FLAG} | docker login --username AWS --password-stdin "$REGISTRY"`,
		).
			Env("REGION", region).
			Env("PROFILE_FLAG", profileFlag).
			Env("REGISTRY", registry).
			Run(); err != nil {
			return err
		}
		sparkwing.Info(ctx, "authenticated with %s", registry)
		return nil
	})
}

func garLoginArgs(host string) []string {
	return []string{"auth", "configure-docker", host, "--quiet"}
}

func garLogin(ctx context.Context, registry string) error {
	host := registryHost(registry)
	args := garLoginArgs(host)
	return step.Run(ctx, "gar login", func(ctx context.Context) error {
		if dryRun() {
			echoArgv(ctx, "gcloud", args)
			return nil
		}
		sparkwing.Info(ctx, "configuring docker auth for %s", host)
		return step.Exec(ctx, "gcloud", args...)
	})
}

func ghcrLoginArgs(host, username string) []string {
	return []string{"login", host, "--username", username, "--password-stdin"}
}

func ghcrLogin(ctx context.Context, cfg LoginConfig) error {
	host := registryHost(cfg.Registry)
	username := cfg.Username
	if username == "" {
		username = defaultGHCRUsername
	}
	args := ghcrLoginArgs(host, username)
	return step.Run(ctx, "ghcr login", func(ctx context.Context) error {
		if dryRun() {
			echoArgv(ctx, "docker", args)
			return nil
		}
		if cfg.TokenSecret == "" {
			return fmt.Errorf("docker.RegistryLogin: ghcr requires TokenSecret")
		}
		token, err := sparkwing.Secret(ctx, cfg.TokenSecret)
		if err != nil {
			return err
		}
		sparkwing.Info(ctx, "authenticating with %s", host)
		// hack: the token goes through .Env() -> stdin so it stays out of
		// argv and the process table; docker login reads --password-stdin.
		if _, err := sparkwing.Bash(
			ctx,
			`printf '%s' "$TOKEN" | docker login "$HOST" --username "$USERNAME" --password-stdin`,
		).
			Env("TOKEN", token).
			Env("HOST", host).
			Env("USERNAME", username).
			Run(); err != nil {
			return err
		}
		sparkwing.Info(ctx, "authenticated with %s", host)
		return nil
	})
}
