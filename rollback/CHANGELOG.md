# Changelog: rollback

All notable changes to the **`github.com/sparkwing-dev/sparks-core/rollback`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `rollback/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

## [v0.3.0] - 2026-08-12

### Added
- **argocd:** `Config.ArgoCD` carries the ArgoCD server and token
  through to `gitops.SyncArgoCD`, which no longer reads them from the
  environment. Leaving it zero keeps the in-cluster probe. See
  [docs/migrations/caller-names-the-registry.md](../docs/migrations/caller-names-the-registry.md).

### Changed
- **deps:** require `gitops` v0.27.0, whose `SyncArgoCD` signature this
  release passes.

### Documentation
- Drop the remaining kind references from `Config`. `Run` has routed on
  `cfg.Local` alone since v0.2.0.

## [v0.2.0] - 2026-08-12

### Changed
- **deps:** bump the sparkwing SDK to v0.31.0, which fixes silent
  truncation of large command output in `Exec(...).Lines()`.

### Removed
- **kind:** (Breaking) `SPARKWING_KIND_CLUSTER` from the routing
  condition in `Run`, which is `cfg.Local` alone now, matching
  `deploy.Run`.

See `docs/migrations/caller-names-the-target.md`.

### Added
- Initial release. `Run` reverts the most recent deployment, routing the
  same way as the deploy package: `kubectl rollout undo` on the
  local/kind path, gitops revert + ArgoCD sync on the remote path.
  Shaped as func(ctx) error to drop into a Job's OnFailure handler when
  a post-deploy Verify reports the new revision unhealthy.
