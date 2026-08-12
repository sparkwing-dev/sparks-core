// Package kube holds kubectl-based deploy helpers that sparks-core
// pipelines chain into their build/push steps. DeployKubectl is the
// primary path: no kustomize, just rollout restart. Apply covers the
// manifest-owning case.
package kube

import (
	"context"
	"os"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// IsRunningInK8s returns true when the current process is executing
// inside a Kubernetes pod (KUBERNETES_SERVICE_HOST set).
func IsRunningInK8s() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// DetectNodeArch returns the architecture of the cluster's nodes as a
// Docker platform string (e.g. "linux/arm64", "linux/amd64"). Queries
// the first node's labels via kubectl. Empty string on failure.
func DetectNodeArch(ctx context.Context) string {
	arch, err := kubectlCapture(ctx, "", "get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.architecture}")
	if err != nil || arch == "" {
		return ""
	}
	return "linux/" + arch
}

// DeployKubectl restarts deployments directly via kubectl rollout
// restart. The deployMap maps image names to k8s deployment names
// (e.g. "myapp" -> "deploy/myapp").
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
