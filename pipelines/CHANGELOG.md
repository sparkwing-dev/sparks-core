# Changelog: pipelines

All notable changes to the **`github.com/sparkwing-dev/sparks-core/pipelines`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `pipelines/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.25.1] - 2026-08-12

### Fixed
- **deps:** require `s3` v0.25.1. v0.25.0 set
  `s3.StaticSiteConfig.ExpectedAccountID` while resolving `s3` v0.24.0,
  which has no such field, so the module did not build outside this
  repo's workspace.

## [v0.25.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.
- **profile:** (Breaking) `StaticDeploy` no longer requires `AWSProfile`. The guard
  existed because an empty profile once became `--profile default`, and
  the `aws` module stopped doing that in `aws/v0.24.0`, so the guard was
  rejecting the configuration CI needs to express: credentials from an
  assumed role, with no profile to name.

  Migration: callers that pass a profile are unaffected. Callers that
  want the aws CLI's own credential chain now leave `AWSProfile` empty
  and set `ExpectedAccountID` instead, so a wrong-account deploy still
  fails before the first write.

### Added
- **identity:** `StaticDeploy.ExpectedAccountID`, passed through to
  `s3.DeployStaticSite`.

### Changed
- **sdk:** bump sparkwing pin to v0.8.0 (gains Job.Verify + failure-aware OnFailure).

## [v0.24.0] - 2026-05-21

### Changed
- BREAKING: bumped sparkwing SDK pin from `v0.2.1` to `v0.4.0`. The
  SDK v0.4.0 reshapes the public surface (package relocations, type
  renames, typed dep interfaces, cache-options renames, risk-label
  consolidation, CLI flag renames). See the migration guide at
  `https://sparkwing.dev/docs/migration-guide/v0.4.0/` and the
  sparks-core root `CHANGELOG.md` for the full surface summary.
- BREAKING: `NextJSBuild` no longer auto-detects laptop vs cluster via the
  retired `sparkwing.CurrentRunConfig().IsLocal`. Strategy is now an explicit
  field on `NextJSBuild` (`"container"` default, `"host"` for the laptop
  fast-path). Existing callers using `NextJSBuild{...}.Apply(&sd)` continue
  to compile and behave as the previous cluster path (container build);
  laptop fast-path becomes opt-in via `Strategy: "host"` -- typically wired
  from a typed `Config` field per pipeline target. `Apply` panics on an
  unknown strategy value (programmer error; surfaces at registration).

## [v0.1.0] - 2026-05-06

### Added
- Initial release. High-level pipeline primitives: DockerDeploy, StaticDeploy, NextJSBuild.
