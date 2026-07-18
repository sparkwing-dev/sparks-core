// Package gcp holds small gcloud-CLI helpers shared across sparks-core
// pipelines: project resolution, Workload Identity detection, service-
// account impersonation, Artifact Registry docker auth, and GKE
// credential bootstrap.
//
// It is the GCP twin of the [github.com/sparkwing-dev/sparks-core/aws]
// module: ProjectArgs mirrors aws.ProfileArgs, IsWorkloadIdentity mirrors
// aws.IsIRSA, and ConfigureDockerAuth mirrors docker.ECRLogin. A reader
// who knows one predicts the other.
//
// Cloud-mutating helpers honor SPARKWING_DRY_RUN: when it is non-empty
// they echo the exact gcloud argv they would run and return success
// without executing, so a scaffolded pipeline goes green locally with no
// GCP credentials. The pure resolution helpers read only environment and
// never shell out.
//
// The gcloud CLI must be on PATH for ConfigureDockerAuth and
// GetGKECredentials.
package gcp

import (
	"os"
)

// projectEnvKeys are the environment variables gcloud itself consults for
// the active project, in precedence order.
var projectEnvKeys = []string{"GOOGLE_CLOUD_PROJECT", "CLOUDSDK_CORE_PROJECT"}

// ResolveProject returns the GCP project id to target. An explicit def
// (typically a pipeline's configured project param) wins; when it is
// empty the GOOGLE_CLOUD_PROJECT and CLOUDSDK_CORE_PROJECT environment
// variables are consulted in that order. An empty result means "let
// gcloud resolve the project from its own config or the metadata server"
// -- see ProjectArgs.
func ResolveProject(def string) string {
	if def != "" {
		return def
	}
	for _, key := range projectEnvKeys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

// ProjectArgs is the argv-shaped project selector: it returns
// {"--project", "<id>"} for a resolved project, or nil when none is
// configured so gcloud falls back to its active config / metadata-server
// project (Application Default Credentials). Append it into a gcloud
// argv directly:
//
//	args := []string{"run", "deploy", service}
//	args = append(args, gcp.ProjectArgs(cfg.Project)...)
//	sparkwing.Exec(ctx, "gcloud", args...).Run()
func ProjectArgs(project string) []string {
	id := ResolveProject(project)
	if id == "" {
		return nil
	}
	return []string{"--project", id}
}

// IsWorkloadIdentity reports whether GCP credentials come from the
// environment (the GKE/GCE metadata server) rather than a key file, the
// GCP analog of aws.IsIRSA. When true, callers must skip key-file auth
// (`gcloud auth activate-service-account`) and let Application Default
// Credentials flow from the metadata server.
//
// An explicit GOOGLE_APPLICATION_CREDENTIALS key file means classic
// service-account-key auth, so it returns false. Otherwise it is true
// when running in-cluster (KUBERNETES_SERVICE_HOST set) -- the GKE
// Workload Identity case, where the pod's ADC is served by the metadata
// server.
func IsWorkloadIdentity() bool {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// ImpersonationArgs returns {"--impersonate-service-account", "<sa>"}
// when CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT names a target service
// account, or nil. Append it into a gcloud argv so every command runs as
// the impersonated identity without a per-call flag.
func ImpersonationArgs() []string {
	sa := os.Getenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT")
	if sa == "" {
		return nil
	}
	return []string{"--impersonate-service-account", sa}
}
