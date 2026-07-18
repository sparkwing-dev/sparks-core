# Changelog: lambda

All notable changes to the **`github.com/sparkwing-dev/sparks-core/lambda`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `lambda/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

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
  AWS credentials.
