# Changelog: ecs

All notable changes to the **`github.com/sparkwing-dev/sparks-core/ecs`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `ecs/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. ECS/Fargate rollout helpers built on the `aws` CLI.
- `Deploy` describes a task-definition family's current revision,
  re-registers it as a fresh revision with the named container's image
  swapped, updates the service, and waits for it to stabilize. It
  returns the prior task-definition ARN so a failed post-deploy check
  can roll back to it.
- `Rollback` points a service back at a prior task-definition revision
  and waits for stability. It is `Job.OnFailure`-shaped: feed it the ARN
  `Deploy` returned.
- Honors the `SPARKWING_DRY_RUN` echo convention (also via
  `DeployConfig.DryRun`): the mutating rollout echoes the exact `aws`
  argv it would run and returns success without contacting AWS, so a dry
  run stays green with no credentials or live service. Profile/IRSA
  resolution comes from the `aws` module.
