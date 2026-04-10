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

// IsECR returns true if the registry URL is an AWS ECR endpoint.
func IsECR(registry string) bool {
	return strings.Contains(registry, ".dkr.ecr.") && strings.Contains(registry, ".amazonaws.com")
}

// ECRRegion extracts the AWS region from an ECR registry URL. Defaults
// to us-west-2 for unrecognized shapes so callers get a deterministic
// fallback rather than an empty string.
func ECRRegion(registry string) string {
	parts := strings.Split(registry, ".")
	if len(parts) > 3 {
		return parts[3]
	}
	return "us-west-2"
}

// ECRLogin authenticates docker with an ECR registry. Safe to call
// repeatedly; the pipe-through-shell shape mirrors the AWS docs so
// failures surface with useful context.
func ECRLogin(ctx context.Context, registry, awsProfile string) error {
	return step.Run(ctx, "ecr login", func(ctx context.Context) error {
		region := ECRRegion(registry)
		profileFlag := aws.ProfileFlag(awsProfile)
		sparkwing.Info(ctx, "authenticating with ECR (region=%s)", region)
		// Pipe is a real bash feature, so this stays as a Bash call;
		// the dynamic values come through .Env() so the shell expands
		// them safely. PROFILE_FLAG is intentionally unquoted so its
		// " --profile <name>" expansion word-splits into two argv
		// tokens (or vanishes when empty).
		if _, err := sparkwing.Bash(ctx,
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

// TryDetectLocalRegistries returns a user-specified local registry via
// SPARKWING_REGISTRY, or nil. Kind clusters no longer host an in-
// cluster registry -- images land via `kind load docker-image` (gated
// by SPARKWING_KIND_CLUSTER in BuildAndPush), so the old port-probe
// heuristic went away.
func TryDetectLocalRegistries(cluster string) ([]string, error) {
	if r := os.Getenv("SPARKWING_REGISTRY"); r != "" && !IsECR(r) {
		return []string{r}, nil
	}
	return nil, nil
}

// DetectLocalRegistries is the strict variant of TryDetectLocalRegistries:
// returns the SPARKWING_REGISTRY override or errors.
func DetectLocalRegistries(cluster string) ([]string, error) {
	if r := os.Getenv("SPARKWING_REGISTRY"); r != "" && !IsECR(r) {
		return []string{r}, nil
	}
	return nil, fmt.Errorf("no local registry configured (set SPARKWING_REGISTRY)")
}

// DetectRegistries returns all registries to push to. SPARKWING_REGISTRY
// wins if set (typical in runner pods). Otherwise uses
// SPARKWING_ECR_REGISTRY, falling back to the provided default.
func DetectRegistries(cluster, defaultECR string) ([]string, error) {
	if r := os.Getenv("SPARKWING_REGISTRY"); r != "" {
		return []string{r}, nil
	}
	ecr := os.Getenv("SPARKWING_ECR_REGISTRY")
	if ecr == "" {
		ecr = defaultECR
	}
	if ecr == "" {
		return nil, fmt.Errorf("no registry configured (set SPARKWING_REGISTRY or SPARKWING_ECR_REGISTRY, or pass defaultECR)")
	}
	return []string{ecr}, nil
}
