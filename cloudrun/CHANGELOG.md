# Changelog: cloudrun

All notable changes to the **`github.com/sparkwing-dev/sparks-core/cloudrun`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `cloudrun/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. Cloud Run deploy, traffic, and rollback orchestration
  behind the gcloud CLI, layered over the `gcp` module:
  - `Deploy`: roll a container image (or, with `Source` set, a source
    tree via Cloud Build buildpacks) out to a service. Returns a
    `DeployResult` with the URL to probe plus the revision that was
    serving beforehand, so a failed verify can roll back precisely.
    Supports port, env vars, public/private access, `--no-traffic`, and
    tagged preview revisions.
  - `DeploySource`: source-based convenience that defaults the source
    directory to the current directory.
  - `ServiceURL`: discover a service's URL by describing it.
  - `Traffic`: a `func(ctx) error` that shifts traffic to a named
    revision or the latest, shaped for a sparkwing Job body or hook.
  - `RollbackToRevision` (alias `Rollback`): a `func(ctx) error` for a
    Job's `OnFailure` hook that shifts all traffic back to a prior
    revision, targeting an explicit revision or discovering the previous
    one at run time.
  - `RemoveTag`: tear down a preview by removing a revision tag.
- Cloud-mutating operations honor `SPARKWING_DRY_RUN` (or a per-call
  `DryRun` field): they echo the exact gcloud argv and return success
  without executing, so a scaffolded pipeline runs green locally with no
  GCP credentials. State reads execute for real.
