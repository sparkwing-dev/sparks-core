// Package gcp holds small gcloud-CLI helpers shared across sparks-core
// pipelines: project resolution, Workload Identity detection, service-
// account impersonation, Artifact Registry docker auth, and GKE
// credential bootstrap.
//
// It is the GCP twin of the [github.com/sparkwing-dev/sparks-core/aws]
// module: ProjectArgs mirrors aws.ProfileArgs, IsWorkloadIdentity mirrors
// aws.IsIRSA, and ConfigureDockerAuth mirrors docker.ECRLogin. A reader
// who knows one predicts the other, with one asymmetry: IsIRSA tests a
// fact (a token file exists) while IsWorkloadIdentity is a heuristic,
// documented on the function.
//
// The caller names the project and the identity. Nothing here reads
// CLOUDSDK_CORE_PROJECT or CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT to
// pick one, because an inherited variable would then redirect a deploy
// without the pipeline changing. gcloud reads both on its own, so
// passing no flag keeps that path with gcloud's own precedence. See the
// environment rules in the repo README.
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

// ProjectArgs is the argv-shaped project selector: it returns
// {"--project", "<id>"} when the caller named a project, and nil
// otherwise, leaving gcloud its own resolution (CLOUDSDK_CORE_PROJECT,
// the active config, or the metadata-server project under Application
// Default Credentials). Append it into a gcloud argv directly:
//
//	args := []string{"run", "deploy", service}
//	args = append(args, gcp.ProjectArgs(cfg.Project)...)
//	sparkwing.Exec(ctx, "gcloud", args...).Run()
func ProjectArgs(project string) []string {
	if project == "" {
		return nil
	}
	return []string{"--project", project}
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
//
// This is a heuristic: KUBERNETES_SERVICE_HOST only proves an in-cluster
// pod, not a GKE one, so it also reports true on non-GKE clusters (EKS,
// kind, self-managed) that have no GCP Workload Identity. Use it only
// where the target cluster is known to be GKE.
func IsWorkloadIdentity() bool {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// ImpersonationArgs returns {"--impersonate-service-account", "<sa>"}
// when the caller named a service account, and nil otherwise. Append it
// into a gcloud argv so the command runs as that identity.
//
// It takes the account as an argument rather than reading
// CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT, because which identity a
// deploy runs as is the caller's statement to make. gcloud honors that
// variable itself when no flag is passed.
func ImpersonationArgs(serviceAccount string) []string {
	if serviceAccount == "" {
		return nil
	}
	return []string{"--impersonate-service-account", serviceAccount}
}
