# container-deploy-ecs-fargate

Test, build a Docker image, push it to ECR, and roll it out to an
ECS/Fargate service. The deploy registers a new task-definition revision
from the running one with the image swapped, updates the service, and
waits for it to stabilize. A post-deploy HTTP probe verifies the new task
set; a definitively unhealthy result rolls the service back to the
previous task definition.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`docker`, `ecs`, `probe`) into a `test -> build ->
deploy` DAG directly, so you can see and edit the orchestration. The
blocks do the work; the scaffolded file is just the shape.

## When to use

- You run a long-lived container on AWS ECS with the Fargate launch type
  (no Kubernetes cluster to manage).
- You want a deploy that rolls itself back when a post-deploy health
  check fails.

## When not to use

- Your service runs on EKS or GKE: use `docker-deploy-ecr-eks` /
  `gke-deploy-gar-kubectl`.
- You want managed serverless containers on GCP: use
  `docker-deploy-gar-cloudrun`.
- The workload is request or event driven and should scale to zero: use
  `lambda-deploy`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `myapp`) |
| `registry` | yes | | Container registry URL to push to (ECR/GAR/GHCR) |
| `cluster` | yes | | ECS cluster name the service runs in |
| `service` | yes | | ECS service name to update |
| `task-family` | yes | | Task-definition family a new revision is registered from |
| `container` | yes | | Container within the task definition whose image is swapped |
| `health-url` | yes | | URL the post-deploy probe checks for a 2xx |
| `region` | yes | | AWS region of the ECS cluster |
| `dockerfile` | no | `Dockerfile` | Path to the Dockerfile, relative to the repo root |
| `platform` | no | `linux/amd64` | Docker build target platform; matches Fargate's default `X86_64` runtime |
| `aws-profile` | no | `` | AWS profile for local runs; empty resolves via `AWS_PROFILE` or IRSA |
| `pipeline-name` | no | `build-test-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command; empty disables the test node |

## Prerequisites

- A `Dockerfile` at the configured `dockerfile` path (default
  `Dockerfile` at the repo root; the `build` node runs `docker build`
  against it).
- An existing ECS service on `cluster` whose task-definition `task-family`
  already has at least one revision. The deploy reads that current
  revision, swaps the image on `container`, and registers a new
  revision from it. It does not create the service or the family.
- `docker` and the `aws` CLI on PATH. Credentials resolve via
  `aws-profile` / `AWS_PROFILE` locally, or IRSA on an in-cluster runner.

## How the rollout works

The `deploy` node calls `ecs.Deploy`, which:

1. describes the current revision of `task-family`,
2. re-registers it as a fresh revision with `container`'s image set
   to the freshly built ECR reference,
3. updates `service` to the new revision, and
4. waits for `aws ecs wait services-stable`.

`ecs.Deploy` returns the prior task-definition ARN, which the pipeline
stashes on its struct. If the post-deploy probe comes back definitively
unhealthy, the `rollback` node points the service back at that captured
ARN with `ecs.Rollback` and waits for it to stabilize again.

An **indeterminate** probe result (the check could not run at all:
connection refused, timeout, or an auth response) is treated differently:
it is not evidence the new revision is bad, so the pipeline leaves the new
revision in place and surfaces the error for a human to investigate rather
than reverting a possibly healthy deploy.

## After rendering

- The probe accepts any 2xx. Tighten it with `.ExpectStatus(200)` or
  `.ExpectJSON("status", "ok")` if your health endpoint returns
  structured output.
- The image is pushed to `registry` and referenced by its
  content-addressed production tag. Adjust `imageRef` if your task
  definition pins a different tag scheme.
- The post-deploy probe retries on a fixed budget
  (`.Retry(30).Interval(2 * time.Second)`, about a minute). That controls
  how long it waits for the new task set to answer; raise it for a slow
  cold start or lower it to fail faster.
- For faster rebuilds, set `CacheFrom` / `CacheTo` on the `build` node's
  `docker.BuildConfig` for a BuildKit registry layer cache. The
  `docker-build-layer-cache` template shows the full wiring.
- Rollback restores the previous task-definition revision. Swap the
  `OnFailure` body for your own recovery if that is not enough.

## Dry run

Set `SPARKWING_DRY_RUN=1` to exercise the pipeline with no AWS access:
the test and build nodes run for real (the build skips its ECR push), and
`ecs.Deploy` / `ecs.Rollback` echo the exact `aws` argv they would run
instead of executing it. The post-deploy probe is skipped in this mode
because nothing was actually rolled out. This is the path the registry
verification harness runs, which is why this template is `dry-runnable`.
