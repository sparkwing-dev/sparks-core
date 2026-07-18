# docker-build-smoketest

Build a Docker image from a Dockerfile and smoke-test that it runs.
Fully local: it needs only a Docker daemon, no registry or cluster.

## Scaffold

```sh
sparkwing pipeline new --name docker-build --template docker-build-smoketest \
  --param image-tag=myapp:local --param dockerfile=Dockerfile --param build-context=.
```

## What it does

One `build` Job, capped by `build-timeout` and re-dispatched once on an
infra-level flake:

1. `docker build -t <image-tag> -f <dockerfile> [--build-arg ...] [--target <stage>] <build-context>`.
2. A `.Verify` postcondition runs the freshly-built image. A container
   that can't start (or exits non-zero) fails the node at the **verify**
   stage, distinct from a build failure.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `docker-build` | pipeline registration name |
| `image-tag` | no | `app:local` | tag to build |
| `dockerfile` | no | `Dockerfile` | Dockerfile path, repo-root-relative |
| `build-context` | no | `.` | build context dir, repo-root-relative |
| `build-args` | no | _(empty)_ | extra `--build-arg` values as `KEY=VAL,KEY=VAL` |
| `target` | no | _(empty)_ | build a specific multi-stage `--target` (empty builds the final stage) |
| `build-timeout` | no | `20m` | max wall-clock for the build-and-smoke-test node (Go duration) |
| `verify-cmd` | no | _(empty)_ | arguments appended to the image's entrypoint for the smoke test |

## Notes

- `verify-cmd` is appended to the image's own entrypoint (`docker run
  --rm <image> <verify-cmd...>`), so the real startup path runs and
  shell-less images (distroless, scratch) work. With it empty the smoke
  test runs the image's default command.
- For a **long-running server image** whose default command never exits,
  set `verify-cmd` to a quick check (e.g. `--version`) so the smoke test
  exits instead of hanging.
- `build-args` mirrors `docker build --build-arg`: use it for base-image
  version pins, feature flags, or a dependency-proxy `PROXY_URL`.
- For pushing to a registry + deploying, use `docker-deploy-ecr-eks`
  (AWS) or `docker-deploy-gar-gke` (GCP). To persist a layer cache across
  runs use `docker-build-layer-cache`.
