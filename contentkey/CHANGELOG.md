# Changelog: contentkey

Versions use [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
and module tags of the form `contentkey/vMAJOR.MINOR.PATCH`.

## [v0.35.0] - 2026-11-10

### Changed

- **deps (Breaking):** resolve pipeline APIs through the independently versioned
  `github.com/sparkwing-dev/sparkwing/sparkwing` module. Upgrade sibling
  sparks-core dependencies together to remove old host-module SDK requirements.
- **tests:** import `github.com/sparkwing-dev/sparkwing/sparkwingtest` from the
  host module. Author tests retain that module and its Go minimum.

## [v0.34.1] - 2026-11-08

### Changed

- **release:** coordinate with the executable template previews.

## [v0.34.0] - 2026-11-08

### Removed

- **contentkey (Breaking):** `OfPaths` and `Salted` are removed. Pass
  `inputs.Files(globs...)`, combined with `inputs.Compose` and `inputs.Parts` for a
  salt, to `JobNode.Memo`; the Sparkwing host hashes the tracked files.

### Changed

- **deps:** pin the Sparkwing SDK to published v0.70.0.

- **contentkey (Breaking):** `OfGoPackage` and `SaltedGoPackage` return
  `sparkwing.MemoKeyFn` for `JobNode.Memo`. They hash `sparkwing.WorkDir()`, the
  process working directory.
- **tests:** run command-backed library tests through `sparkwingtest.Host`.


## [v0.33.6] - 2026-11-05

### Changed

- **deps:** pin the Sparkwing SDK to published v0.66.1.

## [v0.33.5] - 2026-10-03

### Changed

- **release:** Coordinated version with deploy and gitops; no runtime changes.

## [v0.33.4] - 2026-10-03

### Changed

- **release:** Coordinated version with gitops and docker; no runtime changes

## [v0.33.3] - 2026-10-03

### Changed

- **release:** Coordinated version with gitops; no runtime changes

## [v0.33.2] - 2026-10-03

## [v0.33.1] - 2026-10-02

## [v0.33.0] - 2026-10-02

## [v0.32.1] - 2026-10-01

### Fixed

- Pin published Sparkwing v0.47.0 for the existing error-returning cache API.

## [v0.32.0] - 2026-09-09

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
