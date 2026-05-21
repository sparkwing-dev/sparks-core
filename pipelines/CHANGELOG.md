# Changelog: pipelines

All notable changes to the **`github.com/sparkwing-dev/sparks-core/pipelines`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `pipelines/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

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
