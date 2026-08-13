# Changelog: aws

All notable changes to the **`github.com/sparkwing-dev/sparks-core/aws`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `aws/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.25.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.
- **profile:** (Breaking) `ProfileArgs` and `ProfileFlag` no longer read
  `AWS_PROFILE`. The caller's profile is the only one they pass. Passing `--profile`
  overrides the credentials already in the environment, so promoting an
  inherited variable into an override could redirect a deploy without
  the pipeline changing.

  Migration: nothing changes for a caller that already names a profile.
  A caller that relied on `AWS_PROFILE` filling an empty argument now
  gets no `--profile`, and the aws CLI reads `AWS_PROFILE` itself with
  its own precedence, so the same profile still applies unless
  environment credentials outrank it. That ordering matches boto3,
  where an explicitly passed profile suppresses the environment
  provider and an `AWS_PROFILE` alone does not.
- **profile:** the parameter is named `profile` rather than
  `defaultProfile`, because it is a pin and never a fallback.

### Added
- **identity:** `CallerIdentityArgs(profile)` builds the argv that prints the account
  id the credentials resolve to. Callers run it before a destructive
  operation and compare against the account they expect, because a
  profile name pins which credentials get selected and not which
  account they belong to.

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
