# docker-build-layer-cache

Build a Docker image with BuildKit layer caching wired for reuse across CI
runs. The build reads prior layers via `--cache-from` and exports fresh
ones via `--cache-to`, so a repeat build replays unchanged layers instead
of rebuilding them. No registry push, no cluster deploy: the artifact is a
locally-tagged image.

This is the build-layer counterpart to the content-addressed job cache. It
speeds up the build itself; `cached-test-suite` skips a whole job.

## Scaffold

Local cache (a host directory, runs with only Docker):

```sh
sparkwing pipeline new --name build --template docker-build-layer-cache \
  --param image-tag=myapp:local --param dockerfile=Dockerfile
```

Registry cache shared across runners (ECR):

```sh
sparkwing pipeline new --name build --template docker-build-layer-cache \
  --param image-tag=myapp:local \
  --param cache-backend=ecr \
  --param cache-ref=123456789012.dkr.ecr.us-west-2.amazonaws.com/app:buildcache
```

Registry cache (GAR):

```sh
sparkwing pipeline new --name build --template docker-build-layer-cache \
  --param image-tag=myapp:local \
  --param cache-backend=gar \
  --param cache-ref=us-west1-docker.pkg.dev/my-project/my-repo/app:buildcache
```

Forward build args (including a dependency-proxy `PROXY_URL`):

```sh
sparkwing pipeline new --name build --template docker-build-layer-cache \
  --param image-tag=myapp:local \
  --param build-args=PROXY_URL=https://proxy.internal,GO_VERSION=1.23
```

## What it does

One `build` Job:

1. `docker.BuildCacheRef` resolves `cache-backend` (`local`, `ecr`, or
   `gar`) and `cache-ref` into the BuildKit `--cache-from` and `--cache-to`
   spec strings. `local` caches to the `.buildx-cache` directory;
   `ecr`/`gar` cache to the registry ref, exporting `mode=max` so every
   layer is stored, not just the final stage.
2. A single BuildKit `docker build` runs with `DOCKER_BUILDKIT=1`, passing
   `--cache-from`, `--cache-to`, any `--build-arg` entries, `-t
   <image-tag>`, `-f <dockerfile>`, and the build context.

On the first run the cache source is empty, so BuildKit builds every layer
and exports the cache. A later run with unchanged layers imports them and
skips the rebuild.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `build` | pipeline registration name |
| `image-tag` | no | `app:local` | tag to build |
| `dockerfile` | no | `Dockerfile` | Dockerfile path, repo-root-relative |
| `build-context` | no | `.` | build context dir, repo-root-relative |
| `cache-backend` | no | `local` | `local` (host dir), `ecr`, or `gar` (registry ref) |
| `cache-ref` | no | _(empty)_ | registry cache ref for `ecr`/`gar`; ignored for `local` |
| `build-args` | no | _(empty)_ | `KEY=VAL,KEY=VAL` forwarded as `--build-arg` |

## Notes

- **`local` runs with only Docker.** The cache lands in `.buildx-cache` at
  the repo root; add that path to `.gitignore`. On a runner, persist or
  restore that directory between runs (a cache volume or the runner's own
  cache mechanism) so the layers actually carry over. Without persistence a
  fresh `local` run still succeeds but starts cold.
- **`ecr` / `gar` share the cache across runners** by storing it in a
  registry ref. Set `cache-ref` to a repository the runner can read and
  write, and make sure the docker client is logged in first (ambient `aws`
  / `gcloud` credentials). These backends reach a real registry, so they
  are not exercised by the local verification path.
- **`--build-arg` forwarding** takes a comma-separated `KEY=VAL` list. Use
  `PROXY_URL` to route package installs through a dependency proxy; add any
  other args your Dockerfile declares (`ARG`).
- **No push.** The build stops at a locally-tagged image. To push a single
  or multi-arch manifest, use `container-publish-multiarch`. To build, push,
  and roll out to a cluster, use a docker-deploy template
  (`docker-deploy-ecr-eks` for AWS/EKS, `docker-deploy-gar-gke` for
  GCP/GKE). To prove the image builds and its container starts (no
  cross-run cache), use `docker-build-smoketest`.
- **Cache export needs BuildKit.** It ships with Docker 23+; on an older
  engine run the build through `docker buildx` with a container-driver
  builder (`docker buildx create --use`).
