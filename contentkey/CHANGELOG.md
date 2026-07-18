# Changelog: contentkey

All notable changes to the **`github.com/sparkwing-dev/sparks-core/contentkey`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `contentkey/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `OfGoPackage` / `SaltedGoPackage` fold the content hash of a Go
  package's same-module dependency closure into a `sparkwing.CacheKey`
  for a node's `.Cache` modifier, so editing the package or any
  same-module package it imports busts the key while an unrelated edit
  does not. `SaltedGoPackage` adds a caller salt and always folds the
  package spec in, so two packages never replay one another's result.
- `GoDeps` returns the repo-relative Go source files in a package's
  same-module dependency closure (its own source, test, and embedded
  files, plus the non-test source of every same-module package it
  transitively imports), resolved with `go list -deps -test`. Paths are
  git pathspecs suitable for `OfPaths` / `Salted`. Files are made
  relative to the module root `go list` reports, so resolution is stable
  regardless of how the OS resolves symlinks in the checkout path.

## [v0.1.0] - 2026-07-18

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
  - Cache keys hash paths in argv-size-bounded batches, so covering
    every tracked file on a large monorepo no longer risks exceeding the
    OS argument-length limit.
  - A tracked file that has been deleted from the working tree but not
    yet staged is dropped from the key (registering as an ordinary
    change) instead of forcing an uncached run.
