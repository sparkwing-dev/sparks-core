# Changelog: lambda

All notable changes to the **`github.com/sparkwing-dev/sparks-core/lambda`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `lambda/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.33.6] - 2026-11-05

### Fixed

- **dry-run:** parse `SPARKWING_DRY_RUN` as a boolean so helpers execute on normal runs
  when the engine exports `false`; empty and invalid values also disable dry run.

### Changed

- **deps:** pin the Sparkwing SDK to published v0.66.1.

## [v0.33.5] - 2026-10-03

### Changed

- **release:** Coordinated version with deploy and gitops; no runtime changes.

## [v0.33.4] - 2026-10-03

### Changed

- **release:** Coordinated version with gitops and docker; no runtime changes

## [v0.33.3] - 2026-10-03

### Changed

- **release:** Coordinated version with gitops; no runtime changes

## [v0.33.2] - 2026-10-03

### Changed

- **deps:** pin the Sparkwing SDK to published v0.66.4.

## [v0.33.1] - 2026-10-02

## [v0.33.0] - 2026-10-02

## [v0.32.1] - 2026-10-01

## [v0.25.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.

## [v0.1.0] - 2026-07-18

### Added
- Initial module. AWS Lambda deploy helpers for both packaging types
  behind one surface:
  - `DeployImage` updates an Image-packaged function to a new
    `--image-uri`, publishes a version, and shifts a named alias to it,
    returning the version the alias pointed at before the shift.
  - `DeployZip` updates a Zip-packaged function, either staging the
    archive through S3 (when `ArtifactBucket` is set) or uploading it
    inline with `--zip-file`, then publishes and shifts the alias the
    same way.
  - `Rollback` shifts an alias back to a prior version, shaped for a
    sparkwing `Job.OnFailure` hook.
- Dry-run contract: every state-mutating aws call logs its exact argv
  and skips execution when the config `DryRun` field is set or the
  `SPARKWING_DRY_RUN` environment variable is non-empty; the
  current-alias read is skipped under dry-run so a dry deploy needs no
  AWS credentials. `RollbackConfig` carries a `DryRun` field too, for
  config-driven dry runs of the paired `OnFailure` rollback.
- `Region` is no longer defaulted to a hardcoded value. An empty
  `Region` omits `--region` and lets the aws CLI resolve it from
  `AWS_REGION`/`AWS_DEFAULT_REGION` (IRSA or the environment), matching
  the ecs module.
- `ImageDeployConfig`/`ZipDeployConfig` gained an `ExtraArgs` field, a
  passthrough appended verbatim to the `update-function-code` call for
  advanced aws flags the module does not model.
