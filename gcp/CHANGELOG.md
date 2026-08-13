# Changelog: gcp

All notable changes to the **`github.com/sparkwing-dev/sparks-core/gcp`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `gcp/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.2.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.
- **project:** (Breaking) `ProjectArgs` no longer reads
  `GOOGLE_CLOUD_PROJECT` or `CLOUDSDK_CORE_PROJECT`. The caller's
  project is the only one it passes, because an inherited variable
  would otherwise redirect a deploy without the pipeline changing.
  gcloud reads both variables itself when no `--project` is passed, so
  the same project still applies with gcloud's own precedence.
- **identity:** (Breaking) `ImpersonationArgs` takes the service account
  as an argument instead of reading
  `CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT`. Which identity a deploy
  runs as is the caller's statement to make. gcloud honors the variable
  itself when no flag is passed.
- **gke:** `GKEConfig.ImpersonateServiceAccount` carries that account
  into `get-credentials`.

### Removed
- **project:** (Breaking) `ResolveProject`. It existed to hold the
  environment fallback that is gone, and a resolver that resolves
  nothing is worse than no resolver. Pass the project to `ProjectArgs`.

See `docs/migrations/caller-names-the-target.md`.

## [v0.1.0] - 2026-07-18

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
