# Changelog: contentkey

All notable changes to the **`github.com/sparkwing-dev/sparks-core/contentkey`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `contentkey/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. Content-addressed cache keys and path-scoped skip
  predicates over tracked files.
  - `OfPaths` / `Salted` fold the content hash of the tracked files
    under a set of git pathspecs into a `sparkwing.CacheKey` for a
    node's `.Cache` modifier; an unchanged tree replays the recorded
    result. `Salted` adds a caller version component to bust every key
    at once.
  - `Unchanged` / `Changed` compare the working tree against a base ref
    with `git diff` and return a skip predicate for a node's `.SkipIf`
    modifier. Both fail safe: a missing base ref or git error runs the
    work rather than skipping it.
  - File sets come from `git ls-files`, so only tracked files are
    hashed and `.gitignore` is honored. Every function reads git state
    only and never mutates the repository.
