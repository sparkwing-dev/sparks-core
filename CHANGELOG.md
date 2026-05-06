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
- [pipelines/CHANGELOG.md](pipelines/CHANGELOG.md)
- [templates/CHANGELOG.md](templates/CHANGELOG.md)

## [Unreleased]

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
