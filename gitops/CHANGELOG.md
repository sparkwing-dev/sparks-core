# Changelog: gitops

All notable changes to the **`github.com/sparkwing-dev/sparks-core/gitops`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `gitops/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.27.0] - 2026-08-12

### Changed
- **argocd:** (Breaking) `SyncArgoCD` takes an `ArgoCDConfig` naming the
  server and token, where it read `SPARKWING_ARGOCD_SERVER` and
  `SPARKWING_ARGOCD_TOKEN`. The token is a credential with nowhere to be
  passed, which forced callers to resolve it through `sparkwing.Secret`
  and `os.Setenv` it back for this package to find. An empty
  `Server` still probes the in-cluster service and still fails closed
  when it is unreachable, because where the code runs is a fact about
  the machine rather than a choice of target. See
  [docs/migrations/caller-names-the-registry.md](../docs/migrations/caller-names-the-registry.md).

### Documentation
- Name the harness channel explicitly on
  `authorizeDeployWithController`: `SPARKWING_NO_VERIFY`,
  `SPARKWING_CONTROLLER`, `SPARKWING_API_TOKEN`, `SPARKWING_JOB_ID`,
  `SPARKWING_PIPELINE` and `SPARKWING_COMMIT` stay environment reads
  because the runner sets them to describe a job it dispatched, not to
  name a target.

## [v0.26.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.

## [v0.25.0] - 2026-07-29

### Added
- `Revert` rolls a gitops deployment back by reverting a commit (the
  last deploy by default) and pushing, so ArgoCD syncs the cluster back
  to the prior image tags. Clones full history and skips controller
  authorization, since a rollback is a recovery action.

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
- Initial release. GitOps deployment with kustomize patching, retry, and ArgoCD sync.
