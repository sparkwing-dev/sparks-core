# sparks-core

A multi-module monorepo of pipeline libraries for [sparkwing](https://github.com/sparkwing-dev/sparkwing).
Each subdirectory is its own Go module with an independent version tag,
so consumers depend only on what they actually use and rarely-changing
libraries can sit at stable versions while rapidly-iterating ones move
freely.

## Modules

| Module | Path | Purpose |
|---|---|---|
| [step](step/) | `github.com/sparkwing-dev/sparks-core/step` | Shared step-banner + error-wrapped shell helpers used across the other modules |
| [aws](aws/) | `github.com/sparkwing-dev/sparks-core/aws` | AWS profile-flag resolution and IRSA detection |
| [docker](docker/) | `github.com/sparkwing-dev/sparks-core/docker` | Docker build, push, multi-registry tagging, ECR auth, registry detection |
| [s3](s3/) | `github.com/sparkwing-dev/sparks-core/s3` | S3 static-site deployment with cache-header conventions |
| [kube](kube/) | `github.com/sparkwing-dev/sparks-core/kube` | Kubernetes deploy helpers (kubectl, kustomize) |
| [gitops](gitops/) | `github.com/sparkwing-dev/sparks-core/gitops` | GitOps deployment with kustomize patching, retry, and ArgoCD sync |
| [deploy](deploy/) | `github.com/sparkwing-dev/sparks-core/deploy` | Deploy orchestrator: routes to gitops+ArgoCD (cluster) or kubectl (local) |
| [rollback](rollback/) | `github.com/sparkwing-dev/sparks-core/rollback` | Rollback dispatcher: kubectl rollout undo (local/kind) or gitops revert + ArgoCD sync (remote) |
| [migrate](migrate/) | `github.com/sparkwing-dev/sparks-core/migrate` | Database schema migrations via the golang-migrate CLI (up, down, force) |
| [services](services/) | `github.com/sparkwing-dev/sparks-core/services` | Ephemeral Docker service containers (Postgres, ...) for integration tests |
| [notify](notify/) | `github.com/sparkwing-dev/sparks-core/notify` | Post deploy/run notifications to an HTTP webhook (Slack-style or arbitrary JSON) |
| [checks](checks/) | `github.com/sparkwing-dev/sparks-core/checks` | Pre-commit checks: formatting, linting, trailing newlines |
| [probe](probe/) | `github.com/sparkwing-dev/sparks-core/probe` | HTTP health probes for post-deploy verification; Check feeds sparkwing Job.Verify, with unhealthy-vs-indeterminate classification |
| [pipelines](pipelines/) | `github.com/sparkwing-dev/sparks-core/pipelines` | High-level pipeline primitives: DockerDeploy, StaticDeploy, NextJSBuild |
| [templates](templates/) | `github.com/sparkwing-dev/sparks-core/templates` | Curated pipeline template registry: deterministic starters consumed by sparkwing pipeline new --template |
| [gcp](gcp/) | `github.com/sparkwing-dev/sparks-core/gcp` | GCP project/auth resolution and Workload Identity detection, twin of the aws module |
| [cloudrun](cloudrun/) | `github.com/sparkwing-dev/sparks-core/cloudrun` | Cloud Run deploy, traffic shifting, URL discovery, and rollback via gcloud |
| [ecs](ecs/) | `github.com/sparkwing-dev/sparks-core/ecs` | ECS/Fargate task-definition rollout, wait-for-stable, and rollback |
| [lambda](lambda/) | `github.com/sparkwing-dev/sparks-core/lambda` | AWS Lambda deploys (container image and zip), version publish, alias shift and rollback |
| [release](release/) | `github.com/sparkwing-dev/sparks-core/release` | Release and publish helpers: version gating, changelog parsing, checksums, GitHub/npm/PyPI publish flows |
| [contentkey](contentkey/) | `github.com/sparkwing-dev/sparks-core/contentkey` | Content-addressed cache keys and skip predicates for path-scoped work |
| [coverage](coverage/) | `github.com/sparkwing-dev/sparks-core/coverage` | Coverage report parsing (Go coverprofile, lcov, cobertura) and threshold gating |
| [terraform](terraform/) | `github.com/sparkwing-dev/sparks-core/terraform` | Terraform init, plan-to-saved-file, apply-saved-plan, and change summaries |
| [dbbackup](dbbackup/) | `github.com/sparkwing-dev/sparks-core/dbbackup` | Database dump, backup upload, and restore helpers for scheduled backups and restore drills |

Each module has its own `CHANGELOG.md` and is tagged independently
under the convention `<module>/vMAJOR.MINOR.PATCH` (e.g.
`pipelines/v0.1.0`).

## Consuming a module

```
go get github.com/sparkwing-dev/sparks-core/pipelines@latest
```

```go
import "github.com/sparkwing-dev/sparks-core/pipelines"
```

Consumers depending on multiple modules track each independently. With
sparkwing's `sparks.yaml` resolver, each module is one entry:

```yaml
libraries:
  - name: sparks-core/pipelines
    source: github.com/sparkwing-dev/sparks-core/pipelines
    version: latest
  - name: sparks-core/checks
    source: github.com/sparkwing-dev/sparks-core/checks
    version: ^v0.1.0
```

Setting `version: latest` causes the resolver to re-query the proxy on
every pipeline run (frequency-always semantics for free).

## Inter-module dependencies

`pipelines` depends on `step`, `aws`, `s3`, `docker`, and `deploy`.
`deploy` depends on `gitops` and `kube`. `s3` depends on `aws`.
`docker` depends on `step` and `aws`. `checks`, `gitops`, and `kube`
each depend on `step`.

Discipline:

- Inter-module deps **must** use published versions, not `replace`
  directives. This guarantees consumers see the same dep graph as
  developers.
- Local development uses `go.work` at the repo root: every module is
  declared via `use ./<name>` and a workspace-level `replace` shadows
  sibling versions. The workspace file is dev-only -- consumers never
  see it.

## Releasing

Releases are cut by this repo's own `release-modules` pipeline (in
`.sparkwing/`). It:

1. Verifies a clean working tree.
2. Reads the module list from `spark.json` and, for a single lockstep
   version, creates one `<module>/<version>` tag per module.
3. Pushes the new tags to origin.

The repo root is intentionally never tagged with a bare `vX.Y.Z` -- that
would expose the umbrella directory as a module and misroute iterated
`go get`. Every module ships at the same version in a given release, and
sparks-core is locked to `v0.x` (the pipeline refuses `v1.0.0+`).

Cut a release:

```
sparkwing run release-modules --version v0.2.0
```

Preview the tags without creating or pushing them:

```
sparkwing run release-modules --version v0.2.0 --preview
```

Each module keeps its own `CHANGELOG.md`; add the new `[<version>]`
heading to every module you are shipping before cutting the release.

## Layout

```
sparks-core/
├── <module>/     one directory per module in the table above:
│                 go.mod, CHANGELOG.md, *.go
├── .sparkwing/   this repo's own pipelines (pre-commit, pre-push,
│                 release-modules) and lint gates
├── spark.json    the authoritative module manifest
├── go.work       # workspace: dev-only, gitignored from consumers
├── CHANGELOG.md  # repo-level index: links every per-module CHANGELOG
└── README.md     # this file
```

Individual changes land in per-module `CHANGELOG.md` files; the root
CHANGELOG indexes them and records repo-wide events.
