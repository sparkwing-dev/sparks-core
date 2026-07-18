# Changelog: templates

All notable changes to the **`github.com/sparkwing-dev/sparks-core/templates`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `templates/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `go-affected-tests` template (category `caching-skip`, verify
  `runnable`): fans one content-cached test job out per listed Go
  package, each keyed on that package's files plus its same-module
  `go list` dependency closure, so editing one package re-runs only that
  package and its dependents while the rest replay their stored pass. It
  fills the per-package-granularity gap between `cached-test-suite` (a
  single whole-suite key) and `test-shards` (parallel split, no cache);
  the `whenToUse` of `cached-test-suite`, `skip-if-paths-unchanged`, and
  `test-shards` now cross-reference it.

## [v0.27.0] - 2026-07-18

### Added
- 24 new templates, growing the registry to 38: container and serverless
  deploys (`container-deploy-ecs-fargate`, `lambda-deploy`,
  `docker-deploy-gar-cloudrun`, `cloudrun-deploy-source`,
  `gke-deploy-gar-kubectl`, `cloud-functions-deploy`,
  `next-preview-deploy-cloudrun`, `canary-deploy-k8s`), release and
  publish flows (`github-release-go`, `npm-publish-package`,
  `pypi-publish-wheel`, `container-publish-multiarch`), polyglot CI
  hygiene (`lint-test-node`, `lint-test-python`), testing strategies
  (`test-matrix`, `coverage-gated-test`), caching and skip patterns
  (`cached-test-suite`, `skip-if-paths-unchanged`,
  `docker-build-layer-cache`), terraform discipline
  (`terraform-plan-pr`, `terraform-apply-gated`), and database
  operations (`db-migrate-updown`, `db-backup-restore-drill`,
  `scheduled-db-backup`). Every template ships verification metadata
  and composes sparks-core block modules; cloud mutations honor the
  `SPARKWING_DRY_RUN` echo convention.
- `node-module` and `python-module` verification fixtures for templates
  whose steps run npm or python tooling.
- `test-shards` and `integration-test-with-service` recategorized to
  `testing-strategies`; `whenToUse` guidance across the catalog now
  cross-references sibling templates for discrimination.
- `static-deploy-gcs-cloudcdn` now sets per-object Cache-Control
  headers (long-lived immutable hashed assets, no-cache HTML) matching
  its S3 twin.
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
- Verification metadata on `Manifest`: `verify` (tier: `runnable` |
  `dry-runnable` | `compile-only`), `verify_params` (a sample value per
  parameter), and `verify_fixture` (`none` | `go-module` | `docker`),
  with `Tier()` / `Fixture()` accessors. Backfilled honestly onto all 14
  templates. The loader now rejects an unknown tier or fixture, a
  required parameter with no `verify_params` sample, and a
  `verify_params` key that names no declared parameter. This drives a
  registry-wide verification harness that scaffolds, compiles, lints,
  explains, and (for runnable templates) runs each template.

Both new templates use the sparkwing v0.8.0 `Job.Verify` postcondition
for the post-deploy health check and a failure-aware `OnFailure` that
branches on `Failure.Stage`, so an unhealthy new revision (verify-stage
failure) triggers the rollback.

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
