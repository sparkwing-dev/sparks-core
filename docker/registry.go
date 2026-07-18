package docker

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// ECRLogin authenticates docker with an ECR registry. It is a thin
// wrapper over RegistryLogin retained for existing callers; new code can
// call RegistryLogin directly with RegistryECR. Honors SPARKWING_DRY_RUN.
func ECRLogin(ctx context.Context, registry, awsProfile string) error {
	return RegistryLogin(ctx, LoginConfig{Kind: RegistryECR, Registry: registry, AWSProfile: awsProfile})
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
