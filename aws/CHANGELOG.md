# Changelog: aws

All notable changes to the **`github.com/sparkwing-dev/sparks-core/aws`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `aws/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Fixed
- Empty profile no longer forces `--profile default`: when `AWS_PROFILE`
  is unset and the configured profile is empty, `ProfileFlag` returns `""`
  and `ProfileArgs` returns nil, so the aws CLI falls through to its
  ambient credential chain (env keys, SSO cache, instance metadata).
  Hosts with no `default` profile previously broke on every call.

### Removed
- BREAKING: the `DefaultProfile` constant. Nothing falls back to the
  `default` profile anymore.

## [v0.24.0] - 2026-05-21

### Changed
- BREAKING: bumped sparkwing SDK pin from `v0.2.1` to `v0.4.0`. The
  SDK v0.4.0 reshapes the public surface (package relocations, type
  renames, typed dep interfaces, cache-options renames, risk-label
  consolidation, CLI flag renames). See the migration guide at
  `https://sparkwing.dev/docs/migration-guide/v0.4.0/` and the
  sparks-core root `CHANGELOG.md` for the full surface summary.

## [v0.1.0] - 2026-05-06

### Added
- Initial release. AWS profile-flag resolution and IRSA detection.
