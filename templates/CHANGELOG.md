# Changelog: templates

All notable changes to the **`github.com/sparkwing-dev/sparks-core/templates`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `templates/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `go-test-build-deploy-k8s` template: a raw-composition test -> build ->
  deploy DAG that builds to ECR and applies the repo's k8s manifests
  with kubectl, with a post-deploy probe and automatic rollout-undo on
  failure.
- `go-test-migrate-deploy-argo` template: a raw-composition
  integration-test -> build -> migrate -> deploy DAG (ephemeral Postgres
  integration tests, golang-migrate, gitops + ArgoCD) with a post-deploy
  probe and automatic gitops revert on failure.
- `Manifest.whenToUse`: a catalog field answering "which template do I
  pick?", populated on every template.

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
- Initial release. Curated pipeline template registry: deterministic starters consumed by sparkwing pipeline new --template.
