# docker-build-smoketest

Build a Docker image from a Dockerfile and smoke-test that it runs.
Fully local — needs only a Docker daemon, no registry or cluster.

## Scaffold

```sh
sparkwing pipeline new --name docker-build --template docker-build-smoketest \
  --param image-tag=myapp:local --param dockerfile=Dockerfile --param build-context=.
```

## What it does

One `build` Job:

1. `docker build -t <image-tag> -f <dockerfile> <build-context>`.
2. A `.Verify` postcondition runs the freshly-built image. A container
   that can't start (or exits non-zero) fails the node at the **verify**
   stage — distinct from a build failure.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `docker-build` | pipeline registration name |
| `image-tag` | no | `app:local` | tag to build |
| `dockerfile` | no | `Dockerfile` | Dockerfile path, repo-root-relative |
| `build-context` | no | `.` | build context dir, repo-root-relative |
| `verify-cmd` | no | _(empty)_ | smoke-test command run via `sh -c` inside the image |

## Notes

- With `verify-cmd` empty the smoke test runs the image's default
  CMD/ENTRYPOINT. For a **long-running server image** that would hang —
  set `verify-cmd` to a quick check (e.g. `myapp --version`) so the
  smoke test exits.
- For pushing to a registry + deploying, use `docker-deploy-ecr-eks`
  (AWS) or `docker-deploy-gar-gke` (GCP).
