# go-test-migrate-deploy-argo

The full stateful-service path: run integration tests against an
ephemeral Postgres, build a Docker image to ECR, run database migrations
against the target database, then deploy via gitops + ArgoCD. A
post-deploy HTTP probe verifies the new revision; an unhealthy result
triggers an automatic gitops revert.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`services`, `migrate`, `docker`, `deploy`, `probe`,
`rollback`) into an `integration-test -> build -> migrate -> deploy` DAG
directly, so you can see and edit the orchestration.

## When to use

- A deploy must run schema migrations, and you ship through a gitops
  repo + ArgoCD.
- You want integration tests to run against a real, throwaway database
  rather than mocks or a shared test DB.
- You want the deploy to roll itself back when a health check fails.

## When not to use

- No migrations: use `docker-deploy-ecr-eks`.
- You apply k8s YAML directly with `kubectl` (no gitops): use
  `go-test-build-deploy-k8s`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `myapp`) |
| `registry` | yes | | Container registry URL to push to |
| `gitops-repo` | yes | | SSH URL of the gitops repo |
| `gitops-path` | yes | | Path within the gitops repo |
| `app-name` | yes | | ArgoCD app name; also `deploy/<app-name>` for rollback |
| `namespace` | yes | | Kubernetes namespace |
| `health-url` | yes | | URL the post-deploy probe checks for a 2xx |
| `postgres-image` | no | `postgres:16-alpine` | Postgres image for the ephemeral test DB; pin to match prod |
| `migrations-dir` | no | `db/migrations` | golang-migrate migrations directory |
| `dsn-secret` | no | `DATABASE_URL` | sparkwing secret holding the target DB DSN |
| `health-retries` | no | `30` | Post-deploy health-probe attempts before declaring unhealthy |
| `dockerfile` | no | `Dockerfile` | Path to the Dockerfile, relative to the build context |
| `pipeline-name` | no | `test-migrate-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test -tags=integration ./...` | Integration test command; runs with `DATABASE_URL` set |

## After rendering

- The integration-test node migrates the ephemeral database before
  running tests, so tests see the real schema. The test command runs
  with `DATABASE_URL` pointed at the throwaway Postgres. Pin
  `postgres-image` to the version you run in production so version-
  specific SQL behaves the same here.
- `migrateProd` resolves the target DSN from the `dsn-secret` secret, so
  it never lands in source. Set that secret in your cluster config.
- The probe accepts any 2xx; tighten with `.ExpectStatus` /
  `.ExpectJSON` as needed. Widen `health-retries` for slower rollouts
  (cold starts, ingress or DNS propagation) to avoid a false unhealthy
  verdict.
- Rollback reverts the last gitops commit and re-syncs ArgoCD. On a kind
  cluster it falls back to `kubectl rollout undo`.
- **Migrations are forward-only and are not reverted on rollback.** The
  DAG runs migrate against the target database before deploy, so a
  deploy that fails its health probe rolls the *image* back to the old
  code while leaving the *new schema* applied. Write backward-compatible
  migrations (expand/contract) so the old code keeps working against the
  migrated database.

The runner needs `docker`, `kubectl`, `git`, and the `migrate` CLI
(https://github.com/golang-migrate/migrate) on PATH. The deploy node
also needs gitops push credentials (`GITHUB_TOKEN`, or an SSH deploy key
for the gitops repo) and ArgoCD access -- `SPARKWING_ARGOCD_SERVER` plus
`SPARKWING_ARGOCD_TOKEN`, unless the deploy runs inside the cluster.
