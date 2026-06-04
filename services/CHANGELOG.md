# Changelog: services

All notable changes to the **`github.com/sparkwing-dev/sparks-core/services`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `services/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. Starts ephemeral backing services in Docker for
  integration tests and tears them down afterward. `With` runs an
  arbitrary container described by a `Spec` (image, env, published port,
  readiness command) and hands the caller the ephemeral host port;
  `WithPostgres` is a convenience that stands up Postgres and yields a
  ready-to-use DSN. Containers are force-removed even when the test
  function fails or the run is cancelled. Only `docker` is required.
