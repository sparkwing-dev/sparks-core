# Changelog: s3

All notable changes to the **`github.com/sparkwing-dev/sparks-core/s3`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `s3/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.25.1] - 2026-08-12

### Fixed
- **deps:** require `aws` v0.25.0. v0.25.0 called
  `aws.CallerIdentityArgs` while still requiring `aws` v0.24.0, so the
  module did not build for anyone outside this repo's workspace.

## [v0.25.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.
- **sdk:** bump sparkwing pin to v0.8.0 (gains Job.Verify + failure-aware OnFailure).
- `DeployStaticSite` accepts an empty `AWSProfile` instead of erroring
  with "AWSProfile required": it passes no `--profile` and leaves the
  aws CLI its own credential chain, which is what an assumed role in CI
  needs. Takes full effect once the `aws` module pin moves past v0.24.0.
- **profile:** (Breaking) `AWSProfile` no longer falls through to
  `AWS_PROFILE`. See `docs/migrations/caller-names-the-target.md`.

### Added
- **identity:** `StaticSiteConfig.ExpectedAccountID` checks the account the
  credentials resolve to before the first write, and refuses the deploy
  on a mismatch. Unset skips the check. A profile name pins which
  credentials get selected and not which account they belong to, and
  under federated auth there is no profile to name, so this is the only
  way to state which account a deploy means.
- **dry-run:** `DeployStaticSite` honors `SPARKWING_DRY_RUN`, logging the argv of
  every pass and writing nothing. Ten other modules already honored it,
  and this one carries the `--delete`.

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
- Initial release. S3 static-site deployment with cache-header conventions.
