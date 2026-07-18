# Changelog: docker

All notable changes to the **`github.com/sparkwing-dev/sparks-core/docker`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `docker/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `RegistryLogin(ctx, LoginConfig)` generalizing ECR auth across three
  registries via `Kind`: `ecr` (AWS), `gar` (GCP `gcloud auth
  configure-docker`), and `ghcr` (token login from a sparkwing secret,
  piped on stdin so it stays out of argv). `ECRLogin` is retained as a
  thin wrapper. Honors `SPARKWING_DRY_RUN` (echoes argv, no exec).
- `BuildxPublish(ctx, BuildxConfig)` for multi-arch publishes: `docker
  buildx build --platform ... --push` emitting a single manifest.
  Forces BuildKit; honors `SPARKWING_DRY_RUN`.
- `BuildConfig` gains `BuildArgs`, `CacheFrom`, and `CacheTo`;
  `BuildAndPush` now emits `--build-arg`/`--cache-from`/`--cache-to` and
  forces `DOCKER_BUILDKIT=1` so BuildKit cache specs are honored.
- `BuildCacheRef(backend, ref)` resolving `local`/`ecr`/`gar` into the
  BuildKit `--cache-from` and `--cache-to` spec strings.

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
- Initial release. Docker build, push, multi-registry tagging, ECR auth, and registry detection.
