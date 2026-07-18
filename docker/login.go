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

// dryRunEnv toggles command-echo mode for every registry-mutating helper
// in this module; a non-empty value skips execution and logs the argv.
const dryRunEnv = "SPARKWING_DRY_RUN"

// defaultGHCRUsername is the docker-login username used for ghcr when
// LoginConfig.Username is empty. A GitHub token authenticates regardless
// of the username, so a placeholder works for classic tokens; override
// Username with the owning account for a fine-grained token.
const defaultGHCRUsername = "x-access-token"

// dryRun reports whether SPARKWING_DRY_RUN is active.
func dryRun() bool {
	return os.Getenv(dryRunEnv) != ""
}

// echoArgv logs the exact command a dry run would have executed. Callers
// return nil after this so a registry-mutating step is a no-op that still
// shows its argv in the log stream.
func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}

// registryHost returns the host portion of a registry URL, dropping any
// trailing repository path (e.g. "ghcr.io/org" -> "ghcr.io").
func registryHost(registry string) string {
	if i := strings.IndexByte(registry, '/'); i >= 0 {
		return registry[:i]
	}
	return registry
}

// RegistryKind selects the authentication backend RegistryLogin uses.
type RegistryKind string

const (
	// RegistryECR authenticates with AWS Elastic Container Registry via
	// `aws ecr get-login-password`.
	RegistryECR RegistryKind = "ecr"
	// RegistryGAR authenticates with Google Artifact Registry by
	// registering gcloud as a docker credential helper.
	RegistryGAR RegistryKind = "gar"
	// RegistryGHCR authenticates with the GitHub Container Registry via a
	// token piped into `docker login`.
	RegistryGHCR RegistryKind = "ghcr"
)

// LoginConfig drives RegistryLogin.
type LoginConfig struct {
	// Kind selects the auth backend. Empty defaults to RegistryECR.
	Kind RegistryKind
	// Registry is the registry host or host/prefix to authenticate with
	// (e.g. an ECR endpoint, "us-west1-docker.pkg.dev/proj/repo", or
	// "ghcr.io/org"). Required.
	Registry string
	// AWSProfile is the profile for ECR logins on local runs; empty
	// resolves via AWS_PROFILE or drops entirely under IRSA. See
	// aws.ProfileFlag. Ignored for gar/ghcr.
	AWSProfile string
	// TokenSecret is the sparkwing secret name holding the registry token
	// for ghcr. Required for ghcr, ignored for ecr/gar (cloud CLI auth).
	TokenSecret string
	// Username is the docker-login username for ghcr. Empty uses
	// defaultGHCRUsername. Ignored for ecr/gar.
	Username string
}

// RegistryLogin authenticates the local docker client with a container
// registry, dispatching on LoginConfig.Kind. It generalizes ECRLogin
// across the three registries sparks-core publishes to: ECR (AWS), GAR
// (GCP), and GHCR (token login).
//
// Under SPARKWING_DRY_RUN the login argv is echoed and no credentials are
// exchanged, so a scaffolded publish pipeline goes green locally with no
// cloud or registry access.
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

// ecrLogin authenticates docker with an ECR registry. The pipe-through-
// shell shape mirrors the AWS docs so failures surface with useful
// context.
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

// garLoginArgs is the gcloud argv garLogin runs.
func garLoginArgs(host string) []string {
	return []string{"auth", "configure-docker", host, "--quiet"}
}

// garLogin registers gcloud as a docker credential helper for a Google
// Artifact Registry host so subsequent pushes authenticate via the active
// gcloud identity.
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

// ghcrLoginArgs is the docker argv ghcrLogin runs; the token arrives on
// stdin rather than argv so it never appears in the echoed command.
func ghcrLoginArgs(host, username string) []string {
	return []string{"login", host, "--username", username, "--password-stdin"}
}

// ghcrLogin authenticates docker with the GitHub Container Registry using
// a token read from LoginConfig.TokenSecret and piped in on stdin.
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
