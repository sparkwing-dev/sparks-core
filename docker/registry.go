package docker

import (
	"context"
	"fmt"
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

// Registries returns the registries to push to: the one the caller
// named, or ecrRegistry when the caller named none. Passing both means
// registry wins, so a pipeline can redirect a push without editing the
// ECR endpoint it also matches on in the gitops repo.
func Registries(registry, ecrRegistry string) ([]string, error) {
	if registry != "" {
		return []string{registry}, nil
	}
	if ecrRegistry == "" {
		return nil, fmt.Errorf("no registry named: pass a registry, an ECR registry, or both")
	}
	return []string{ecrRegistry}, nil
}

// LocalRegistries returns registry as a one-element list when it names a
// local (non-ECR) registry, and nil otherwise. A local registry is
// optional, so naming none is not an error; use [RequireLocalRegistry]
// when it is.
func LocalRegistries(registry string) []string {
	if registry != "" && !IsECR(registry) {
		return []string{registry}
	}
	return nil
}

// RequireLocalRegistry is [LocalRegistries] for a caller that cannot
// proceed without one: it errors when registry is empty or names an ECR
// endpoint.
func RequireLocalRegistry(registry string) ([]string, error) {
	if local := LocalRegistries(registry); local != nil {
		return local, nil
	}
	if registry == "" {
		return nil, fmt.Errorf("no local registry named")
	}
	return nil, fmt.Errorf("registry %q is an ECR endpoint, not a local registry", registry)
}
