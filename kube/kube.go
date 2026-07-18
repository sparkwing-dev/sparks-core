// Package kube holds kubectl-based deploy helpers that sparks-core
// pipelines chain into their build/push steps. Two primary paths:
//
//   - DeployKindKustomize: the repo owns its k8s manifests; we patch
//     image tags and kubectl apply -k against a kind cluster.
//   - DeployKubectl: no kustomize; just rollout restart.
package kube

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// KindKustomizeConfig drives DeployKindKustomize. Inputs are the
// cluster name, the path to the kustomization directory (the repo's
// own kind manifests), the set of images being rolled, the new tag,
// the image->deployment map (for rollout-status waits), and the
// namespace (for kubectl context).
type KindKustomizeConfig struct {
	Cluster      string
	KustomizeDir string
	Images       []string
	Tag          string
	DeployMap    map[string]string
	Namespace    string
}

// DeployKindKustomize is the repo-owns-its-manifests deploy path for
// kind clusters. Steps: patch kustomization.yaml, kubectl apply -k,
// rollout status on each mapped deployment that actually exists.
//
// Images should match what `kind load docker-image` pushed into the
// cluster -- short names like "myapp" (not registry-prefixed).
func DeployKindKustomize(ctx context.Context, cfg KindKustomizeConfig) error {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	kubeCtx := "kind-" + cfg.Cluster
	return step.Run(ctx, "deploy (kind kustomize)", func(ctx context.Context) error {
		kustPath := filepath.Join(cfg.KustomizeDir, "kustomization.yaml")
		if err := patchKustomizationImages(kustPath, cfg.Images, cfg.Tag); err != nil {
			return fmt.Errorf("patch kustomization.yaml: %w", err)
		}
		sparkwing.Info(ctx, "applying %s", cfg.KustomizeDir)
		if err := kubectl(ctx, kubeCtx, "apply", "-k", cfg.KustomizeDir); err != nil {
			return err
		}
		for _, img := range cfg.Images {
			deploy, ok := cfg.DeployMap[img]
			if !ok {
				continue
			}
			out, _ := kubectlCapture(ctx, kubeCtx,
				"get", deploy,
				"-n", cfg.Namespace,
				"--ignore-not-found",
				"-o", "name",
			)
			if out == "" {
				continue
			}
			sparkwing.Info(ctx, "waiting for %s rollout", deploy)
			if err := kubectl(ctx, kubeCtx, "rollout", "status",
				deploy, "-n", cfg.Namespace, "--timeout=180s"); err != nil {
				return err
			}
		}
		return nil
	})
}

// patchKustomizationImages rewrites the `images:` section of a
// kustomization.yaml so every image in `images` has newTag=tag.
// Unknown images are appended; known ones have their newTag
// replaced. The rest of the file is preserved (resources, patches,
// labels, etc.).
//
// Hand-rolled patching rather than yaml.v3 parsing because
// kustomization.yaml uses specific indent + ordering conventions
// that kubectl is picky about; the simple line-based approach
// matches prod patterns elsewhere in sparks-core.
func patchKustomizationImages(path string, images []string, tag string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")

	imagesStart := -1
	imagesEnd := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if imagesStart == -1 && trimmed == "images:" {
			imagesStart = i
			continue
		}
		if imagesStart != -1 {
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && line[0] != '-' && line[0] != '#' {
				imagesEnd = i
				break
			}
		}
	}
	if imagesStart == -1 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "images:")
		imagesStart = len(lines) - 1
		imagesEnd = len(lines)
	} else if imagesEnd == -1 {
		imagesEnd = len(lines)
	}

	seen := map[string]bool{}
	for i := imagesStart + 1; i < imagesEnd; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(trimmed, "- name:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "- name:"))
		seen[name] = true
		for j := i + 1; j < imagesEnd; j++ {
			t := strings.TrimSpace(lines[j])
			if strings.HasPrefix(t, "- name:") {
				break
			}
			if strings.HasPrefix(t, "newTag:") {
				lines[j] = "    newTag: " + tag
				break
			}
		}
	}

	var newEntries []string
	for _, img := range images {
		if seen[img] {
			continue
		}
		newEntries = append(newEntries, "  - name: "+img, "    newTag: "+tag)
	}
	if len(newEntries) > 0 {
		tail := append([]string{}, lines[imagesEnd:]...)
		lines = append(lines[:imagesEnd], newEntries...)
		lines = append(lines, tail...)
	}

	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
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

// DeployKustomizeConfig configures kustomize-based deployment for
// local/Kind clusters via the user's ~/.sparkwing/clusters tree.
// Legacy path kept for callers that haven't migrated to
// DeployKindKustomize with an in-repo manifests directory.
type DeployKustomizeConfig struct {
	Images    []string
	Tag       string
	Cluster   string
	Namespace string
	Registry  string
	DeployMap map[string]string
}

// DeployKustomize updates a cluster's kustomization.yaml with pinned
// image tags and applies via kubectl apply -k. Falls back to kubectl
// rollout restart if the kustomization.yaml doesn't exist.
func DeployKustomize(ctx context.Context, cfg DeployKustomizeConfig) error {
	if cfg.Namespace == "" {
		cfg.Namespace = "sparkwing"
	}
	if cfg.Registry == "" {
		cfg.Registry = "localhost:30500"
	}

	return step.Run(ctx, "deploy (kustomize)", func(ctx context.Context) error {
		homeDir, _ := os.UserHomeDir()
		kustomizePath := filepath.Join(homeDir, ".sparkwing", "clusters", cfg.Cluster, "k8s", cfg.Namespace, "kustomization.yaml")

		kubeCtx := ""
		if !IsRunningInK8s() {
			kubeCtx = "kind-" + cfg.Cluster
		}

		data, err := os.ReadFile(kustomizePath)
		if err != nil {
			sparkwing.Info(ctx, "warning: %s not found - falling back to rollout restart", kustomizePath)
			for _, img := range cfg.Images {
				if deploy, ok := cfg.DeployMap[img]; ok {
					if err := kubectl(ctx, kubeCtx, "rollout", "restart", deploy, "-n", cfg.Namespace); err != nil {
						return err
					}
				}
			}
			return nil
		}

		content := string(data)
		for _, img := range cfg.Images {
			fullName := cfg.Registry + "/" + img
			altName := "localhost:30500/" + img
			pattern := fmt.Sprintf("name: %s\n    newTag: ", fullName)
			idx := strings.Index(content, pattern)
			if idx == -1 && altName != fullName {
				pattern = fmt.Sprintf("name: %s\n    newTag: ", altName)
				idx = strings.Index(content, pattern)
			}
			if idx == -1 {
				sparkwing.Info(ctx, "  %s: not in kustomization (using manifest default)", img)
				continue
			}
			after := idx + len(pattern)
			eol := strings.Index(content[after:], "\n")
			if eol == -1 {
				eol = len(content[after:])
			}
			content = content[:after] + cfg.Tag + content[after+eol:]
			sparkwing.Info(ctx, "  %s -> %s", img, cfg.Tag)
		}

		if err := os.WriteFile(kustomizePath, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write kustomization.yaml: %w", err)
		}

		kustomizeDir := filepath.Dir(kustomizePath)
		sparkwing.Info(ctx, "applying kustomization from %s", kustomizeDir)
		if err := kubectl(ctx, kubeCtx, "apply", "-k", kustomizeDir); err != nil {
			return err
		}
		sparkwing.Info(ctx, "kustomization applied")

		for _, img := range cfg.Images {
			deploy, ok := cfg.DeployMap[img]
			if !ok {
				continue
			}
			sparkwing.Info(ctx, "waiting for %s rollout", deploy)
			if err := kubectl(ctx, kubeCtx, "rollout", "status", deploy, "-n", cfg.Namespace, "--timeout=180s"); err != nil {
				return err
			}
		}
		return nil
	})
}
