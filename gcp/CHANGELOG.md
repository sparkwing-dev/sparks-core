# Changelog: gcp

All notable changes to the **`github.com/sparkwing-dev/sparks-core/gcp`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `gcp/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. gcloud-CLI auth and project helpers, the GCP twin of
  the `aws` module:
  - `ResolveProject` / `ProjectArgs`: resolve the target project from an
    explicit value or the `GOOGLE_CLOUD_PROJECT` / `CLOUDSDK_CORE_PROJECT`
    environment, emitting `--project <id>` or nil (ADC fallback).
  - `IsWorkloadIdentity`: detect metadata-server credentials (GKE
    Workload Identity), the analog of `aws.IsIRSA`, so callers skip
    key-file auth.
  - `ImpersonationArgs`: emit `--impersonate-service-account` from
    `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT`.
  - `ConfigureDockerAuth`: register gcloud as a docker credential helper
    for an Artifact Registry host, deduplicated per host, the twin of
    `docker.ECRLogin`.
  - `GetGKECredentials`: bootstrap a kubeconfig context for the `kube`
    block via `gcloud container clusters get-credentials`.
- Cloud-mutating helpers honor `SPARKWING_DRY_RUN`: when it is non-empty
  they echo the exact gcloud argv and return success without executing,
  so a scaffolded pipeline runs green locally with no GCP credentials.
- `GKEConfig.ExtraArgs`: extra flags appended verbatim to the
  `get-credentials` argv, the escape hatch for private control planes
  (`--internal-ip` / `--dns-endpoint`) and other flags the struct does
  not model.

### Fixed
- `ConfigureDockerAuth` no longer shares a plain `map[string]error`
  across goroutines: configuring different Artifact Registry hosts
  concurrently could trigger a fatal concurrent map read/write. Each
  host's result now lives in a per-host struct synchronized by its own
  `sync.Once`.
