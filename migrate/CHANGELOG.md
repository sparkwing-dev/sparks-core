# Changelog: migrate

All notable changes to the **`github.com/sparkwing-dev/sparks-core/migrate`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `migrate/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. Runs golang-migrate database migrations as a
  sparkwing step by shelling out to the `migrate` CLI. `Up` applies all
  pending migrations, `Down` rolls back N (or all), `Force` clears the
  dirty flag for recovery. `Config` carries the migrations directory and
  DSN; the `migrate` binary is expected on PATH.
