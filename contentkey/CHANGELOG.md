# Changelog: contentkey

Versions use [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
and module tags of the form `contentkey/vMAJOR.MINOR.PATCH`.

## [v0.4.0]

### Changed

- **Breaking:** `OfPaths`, `Salted`, `OfGoPackage`, and `SaltedGoPackage`
  return `sparkwing.CacheKeyFn` with `(sparkwing.CacheKey, error)` results.
  Direct callers must check the error. `Memoize` callers receive resolution
  failures through the SDK and must use its error-returning cache API.
- Hashing and Go dependency failures retain their causes and fail resolution.
  Successful keys, including keys for unstaged deletions, retain their format.
- Cache resolution requires `sparkwing.WorkDir()` to identify the project.
  Standalone callers and test fixtures must bind it with `sparkwing.SetWorkDir`.
  Go package keys require one target main module and resolvable source paths.
- Nested workspace package keys anchor dependency paths to the project root.
  Extra pathspecs remain project-relative.

## [v0.3.1] - 2026-08-12

### Fixed

- Updated cache modifier examples to `.Memoize(...)`.

### Changed

- Raised the Sparkwing dependency to v0.32.1.

## [v0.3.0] - 2026-08-12

### Changed

- Raised the Sparkwing dependency to v0.31.0 for complete command output.

### Fixed

- Distinguished unstaged deletions from other file inspection failures.
  Other failures produced an uncached result in this version.

## [v0.2.0] - 2026-07-18

### Added

- Package cache keys include same-module source dependencies and target tests.
- `SaltedGoPackage` includes the package specification in the key.
- `GoDeps` returns module-relative source, test, and embedded file paths.

## [v0.1.0] - 2026-07-18

### Added

- Tracked-file cache keys, caller salts, and path-scoped change predicates.
- Batched hashing within the operating system argument limit.
- Unstaged deletions change the key by removing the deleted path.
