# Changelog

All notable changes to **sparks-core** are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

sparks-core is a multi-module monorepo: each subdirectory has its own
`CHANGELOG.md` and version tags. See the per-module CHANGELOGs:

- [step/CHANGELOG.md](step/CHANGELOG.md)
- [aws/CHANGELOG.md](aws/CHANGELOG.md)
- [docker/CHANGELOG.md](docker/CHANGELOG.md)
- [s3/CHANGELOG.md](s3/CHANGELOG.md)
- [kube/CHANGELOG.md](kube/CHANGELOG.md)
- [gitops/CHANGELOG.md](gitops/CHANGELOG.md)
- [deploy/CHANGELOG.md](deploy/CHANGELOG.md)
- [checks/CHANGELOG.md](checks/CHANGELOG.md)
- [probe/CHANGELOG.md](probe/CHANGELOG.md)
- [pipelines/CHANGELOG.md](pipelines/CHANGELOG.md)
- [templates/CHANGELOG.md](templates/CHANGELOG.md)

## [Unreleased]

## [v0.24.0] - 2026-05-21

### Changed
- BREAKING: bumped sparkwing SDK pin across every sub-module from `v0.2.1`
  to `v0.4.0`. The SDK v0.4.0 reshapes the public surface: package
  relocations (`orchestrator/` -> `pkg/runner`, `secrets/` -> internal,
  `logs/` -> public, etc.), DAG-vertex type renames (`*Node` ->
  `*JobNode`), runner-selection method rename (`RunsOn` -> `Requires`),
  typed dep interfaces (`Needs(...any)` -> typed `Dep` / `WorkDep`),
  `CacheOptions.Key` -> `Namespace`, `CacheOptions.CacheKey` ->
  `ContentHash`, risk-label consolidation
  (`Destructive`/`AffectsProduction`/`CostsMoney` -> `Risk(labels...)`),
  CLI flag renames (`--sw-on` -> `--sw-profile`, `--sw-for` ->
  `--sw-target`, `--sw-allow-destructive` -> `--sw-allow destructive`,
  ...), and several smaller surface cleanups. Consumers of sparks-core
  need to re-migrate their pipeline code against the v0.4.0 surface; the
  full migration guide is at
  `https://sparkwing.dev/docs/migration-guide/v0.4.0/`.
- Synchronized release across root + all 10 sub-modules at `v0.24.0`.
  Inter-sub-module require lines bumped from `v0.23.0` to `v0.24.0` so
  the family is internally consistent under one tag.

## [v0.21.0] - 2026-05-06

### Added
- Root `go.mod` declaring `module github.com/sparkwing-dev/sparks-core`.
  Sparks-core remains a multi-module monorepo (each subdirectory has its
  own `go.mod` and is versioned independently); the root module is an
  empty umbrella whose only job is to give `proxy.golang.org` a valid
  module declaration to serve when Go's "matching version" logic
  auto-fetches the parent alongside any sub-module request. Without it,
  consumer `go get sparks-core/<sub>@vX.Y.Z` calls fall back to stale
  proxy-cached blobs from earlier rename attempts.

### Changed
- Synchronized release across root + all 10 sub-modules at `v0.21.0`.
  Climbing past the proxy's max-cached top-level version (`v0.20.0`)
  ensures fresh resolution. Earlier `v0.4.0` tags created on 2026-05-06
  are functional dead-letters: sub-module tags are clean, but the
  matching parent `sparks-core@v0.4.0` is poisoned in the proxy cache.
- `go.work` now includes the root module (`use .`) and drops the stale
  `v0.3.0` per-module `replace` directives that no longer apply.

## [v0.1.0] - 2026-05-06

### Added
- Initial release. Multi-module monorepo of pipeline libraries for sparkwing,
  with each module versioned independently.
