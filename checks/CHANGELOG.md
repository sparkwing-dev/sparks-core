# Changelog: checks

All notable changes to the **`github.com/sparkwing-dev/sparks-core/checks`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `checks/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `Comments(ctx, paths...)`: gates Go source against the comment policy
  (godoc on declarations plus the `// hack:` / `// safety:` / `// bug:` /
  `// perf:` tag allowlist; everything else rejected). Paths default to
  the repo root and resolve relative to `sparkwing.WorkDir()`; pass
  specific subdirectories to scope the gate.

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

## [v0.1.0] - 2026-05-06

### Added
- Initial release. Pre-commit checks: formatting, linting, and trailing newlines.
