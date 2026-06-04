# Changelog: rollback

All notable changes to the **`github.com/sparkwing-dev/sparks-core/rollback`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `rollback/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. `Run` reverts the most recent deployment, routing the
  same way as the deploy package: `kubectl rollout undo` on the
  local/kind path, gitops revert + ArgoCD sync on the remote path.
  Shaped as func(ctx) error to drop into a Job's OnFailure handler when
  a post-deploy Verify reports the new revision unhealthy.
