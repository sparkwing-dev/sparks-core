// Package kube holds kubectl-based deploy helpers: DeployKubectl for a
// rollout restart, Apply for repos that own their manifests.
package kube

import (
	"context"
	"os"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// IsRunningInK8s reports whether the process is executing inside a pod.
func IsRunningInK8s() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// DetectNodeArch returns the first node's architecture as a Docker platform
// string, or "" on failure.
func DetectNodeArch(ctx context.Context) string {
	arch, err := kubectlCapture(ctx, "", "get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.architecture}")
	if err != nil || arch == "" {
		return ""
	}
	return "linux/" + arch
}

// DeployKubectl restarts the deployments deployMap names for each image.
func DeployKubectl(ctx context.Context, images []string, deployMap map[string]string, namespace string) error {
	if namespace == "" {
		namespace = "sparkwing"
	}
	return step.Run(ctx, "deploy (kubectl)", func(ctx context.Context) error {
		for _, img := range images {
			deploy, ok := deployMap[img]
			if !ok {
				continue
			}
			sparkwing.Info(ctx, "restarting %s", deploy)
			if err := kubectl(ctx, "", "rollout", "restart", deploy, "-n", namespace); err != nil {
				return err
			}
		}
		return nil
	})
}
