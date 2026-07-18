# Changelog: release

All notable changes to the **`github.com/sparkwing-dev/sparks-core/release`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `release/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial module. Shared release-and-publish helpers behind the
  language-registry publish templates (github-release-go,
  npm-publish-package, pypi-publish-wheel):
  - `DeriveVersion` resolves a release version from an explicit
    parameter or a git tag, validates Semantic Versioning (with an
    optional leading `v`), and can refuse a dirty working tree. `IsSemver`
    exposes the validation on its own.
  - `GuardVersionFile` reads the declared version from a `package.json`
    (`version`) or `pyproject.toml` (`project.version`, `tool.poetry.version`)
    and errors if the matching git tag already exists, so a re-publish
    fails fast. Shared verbatim by the npm and PyPI templates.
  - `ChangelogEntry` extracts the notes body and version for one section
    of a Keep a Changelog file, defaulting to the top released section.
  - `CrossBuildGo` compiles a GOOS/GOARCH matrix into a dist directory
    and returns the artifact paths; `ArtifactPath` names one entry.
    `Checksums` writes a sha256sum-format manifest over the artifacts.
  - `GitHubRelease`, `NpmPublish`, and `PyPIPublish` wrap
    `gh release create`, `npm publish`, and `twine upload` / `uv publish`.
    Each honors the `SPARKWING_DRY_RUN` convention (and a per-call
    `DryRun` field): under a dry run the command argv is echoed and the
    step returns success without reaching the registry, so a freshly
    scaffolded pipeline runs green with no token. State-reading and local
    build helpers always execute for real.
