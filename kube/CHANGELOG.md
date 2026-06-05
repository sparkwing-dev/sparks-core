# Changelog: kube

All notable changes to the **`github.com/sparkwing-dev/sparks-core/kube`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `kube/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- `Apply` runs a raw `kubectl apply` against one or more manifest paths
  (optionally server-side) and waits on named deployments -- a
  plain-YAML deploy path alongside the kustomize and rollout-restart
  helpers.
- `SetImage` points a deployment's container at a new image via
  `kubectl set image` and waits for the rollout, so a CD pipeline can
  roll the freshly built content-addressed tag (and `RolloutUndo` can
  later step back to the prior tag).
- `RolloutUndo` rolls deployments back to their previous ReplicaSet via
  `kubectl rollout undo` and waits for the rollback to complete. Takes a
  `RolloutUndoConfig` with an explicit `Context`, so a rollback targets
  the same cluster the deploy did rather than the current kubeconfig
  context.

- `ResolveContext` centralizes kube context selection for every kubectl
  call in the package and **fails closed**: explicit `Context` >
  in-cluster service account > `SPARKWING_KUBE_CONTEXT` >
  `kind-$SPARKWING_KIND_CLUSTER` > `SPARKWING_KUBE_ALLOW_CURRENT=1` >
  error. No command silently falls through to the current kubeconfig
  context (which may be the wrong cluster). Every kubectl call -- write
  (apply, set image, rollout) and read (`DetectNodeArch`, rollout-status
  lookups) -- now routes through this single helper; none use the ambient
  context implicitly.

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
- Initial release. Kubernetes deploy helpers (kubectl, kustomize).
