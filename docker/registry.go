package docker

import (
	"context"
	"fmt"
	"strings"
)

func IsECR(registry string) bool {
	return strings.Contains(registry, ".dkr.ecr.") && strings.Contains(registry, ".amazonaws.com")
}

// ECRRegion extracts the AWS region from an ECR registry URL, falling back
// to us-west-2 for an unrecognized shape.
func ECRRegion(registry string) string {
	parts := strings.Split(registry, ".")
	if len(parts) > 3 {
		return parts[3]
	}
	return "us-west-2"
}

// ECRLogin is RegistryLogin with RegistryECR, retained for existing callers.
func ECRLogin(ctx context.Context, registry, awsProfile string) error {
	return RegistryLogin(ctx, LoginConfig{Kind: RegistryECR, Registry: registry, AWSProfile: awsProfile})
}

// Registries returns registry when named, else ecrRegistry, so a pipeline
// can redirect a push without editing the ECR endpoint gitops matches on.
func Registries(registry, ecrRegistry string) ([]string, error) {
	if registry != "" {
		return []string{registry}, nil
	}
	if ecrRegistry == "" {
		return nil, fmt.Errorf("no registry named: pass a registry, an ECR registry, or both")
	}
	return []string{ecrRegistry}, nil
}

// LocalRegistries returns registry when it names a local (non-ECR) one, and
// nil otherwise. See [RequireLocalRegistry] when one is mandatory.
func LocalRegistries(registry string) []string {
	if registry != "" && !IsECR(registry) {
		return []string{registry}
	}
	return nil
}

// RequireLocalRegistry is [LocalRegistries] that errors instead of
// returning nil.
func RequireLocalRegistry(registry string) ([]string, error) {
	if local := LocalRegistries(registry); local != nil {
		return local, nil
	}
	if registry == "" {
		return nil, fmt.Errorf("no local registry named")
	}
	return nil, fmt.Errorf("registry %q is an ECR endpoint, not a local registry", registry)
}
