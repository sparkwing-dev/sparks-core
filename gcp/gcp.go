// Package gcp holds gcloud-CLI helpers: project resolution, Workload
// Identity detection, service-account impersonation, Artifact Registry
// docker auth, and GKE credential bootstrap. It is the GCP twin of the
// aws module. Nothing here reads CLOUDSDK_CORE_PROJECT or
// CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT, because an inherited variable
// would redirect a deploy without the pipeline changing; passing no flag
// leaves gcloud its own precedence. Cloud-mutating helpers honor
// SPARKWING_DRY_RUN.
package gcp

import (
	"os"
)

// ProjectArgs returns {"--project", project}, or nil for an empty project,
// which leaves gcloud its own resolution.
func ProjectArgs(project string) []string {
	if project == "" {
		return nil
	}
	return []string{"--project", project}
}

// IsWorkloadIdentity reports whether credentials come from the metadata
// server rather than a key file, meaning callers must skip
// `gcloud auth activate-service-account`. It is a heuristic:
// KUBERNETES_SERVICE_HOST proves an in-cluster pod, not a GKE one, so it
// also reports true on EKS, kind, and self-managed clusters.
func IsWorkloadIdentity() bool {
	if os.Getenv("GOOGLE_APPLICATION_CREDENTIALS") != "" {
		return false
	}
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}

// ImpersonationArgs returns {"--impersonate-service-account",
// serviceAccount}, or nil for an empty account.
func ImpersonationArgs(serviceAccount string) []string {
	if serviceAccount == "" {
		return nil
	}
	return []string{"--impersonate-service-account", serviceAccount}
}
