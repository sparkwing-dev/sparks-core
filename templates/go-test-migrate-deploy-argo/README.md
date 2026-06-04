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
| `ecr` | yes | | ECR registry URL |
| `gitops-repo` | yes | | SSH URL of the gitops repo |
| `gitops-path` | yes | | Path within the gitops repo |
| `app-name` | yes | | ArgoCD app name; also `deploy/<app-name>` for rollback |
| `namespace` | yes | | Kubernetes namespace |
| `health-url` | yes | | URL the post-deploy probe checks for a 2xx |
| `migrations-dir` | no | `db/migrations` | golang-migrate migrations directory |
| `db-secret` | no | `DATABASE_URL` | sparkwing secret holding the target DB DSN |
| `pipeline-name` | no | `test-migrate-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test -tags=integration ./...` | Integration test command; runs with `DATABASE_URL` set |

## After rendering

- The integration-test node migrates the ephemeral database before
  running tests, so tests see the real schema. The test command runs
  with `DATABASE_URL` pointed at the throwaway Postgres.
- `migrateProd` resolves the target DSN from the `db-secret` secret, so
  it never lands in source. Set that secret in your cluster config.
- The probe accepts any 2xx; tighten with `.ExpectStatus` /
  `.ExpectJSON` as needed.
- Rollback reverts the last gitops commit and re-syncs ArgoCD. On a kind
  cluster it falls back to `kubectl rollout undo`.

The runner needs `docker`, `kubectl`, `git`, and the `migrate` CLI
(https://github.com/golang-migrate/migrate) on PATH.
