# container-publish-multiarch

Build a multi-arch (amd64 + arm64) container image with `docker buildx`
and push a single manifest to a registry (ECR, GAR, or GHCR). No cluster
deploy: the pipeline stops at a pushed image. This is the publish-half of
the docker-deploy templates.

## Scaffold

```sh
sparkwing pipeline new --name publish-image --template container-publish-multiarch \
  --param image=myapp \
  --param registry=633280902600.dkr.ecr.us-west-2.amazonaws.com \
  --param registry-kind=ecr \
  --param tags=1.4.2,latest
```

GAR:

```sh
sparkwing pipeline new --name publish-image --template container-publish-multiarch \
  --param image=myapp \
  --param registry=us-west1-docker.pkg.dev/my-project/my-repo \
  --param registry-kind=gar \
  --param tags=1.4.2
```

GHCR (token login):

```sh
sparkwing pipeline new --name publish-image --template container-publish-multiarch \
  --param image=org/myapp \
  --param registry=ghcr.io/org \
  --param registry-kind=ghcr \
  --param token-secret=GITHUB_TOKEN \
  --param tags=1.4.2
```

## What it does

One `publish` Job (with a 45-minute timeout, since a cross-arch build is
slow):

1. `docker.RegistryLogin` authenticates the local docker client, dispatching
   on `registry-kind`: `ecr` runs `aws ecr get-login-password | docker login`,
   `gar` runs `gcloud auth configure-docker`, `ghcr` pipes the `token-secret`
   value into `docker login --password-stdin`.
2. `docker.BuildxPublish` runs `docker buildx build --platform <platforms>
   --push`, building every requested architecture, reading/writing a BuildKit
   layer cache, and pushing one multi-arch manifest for each of `tags` to
   `<registry>/<image>`.

No smoke-test, no deploy: the artifact is the pushed manifest.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-image` | pipeline registration name |
| `image` | yes | | image name / repo path within the registry |
| `registry` | yes | | container registry URL to push to |
| `registry-kind` | no | `ecr` | auth dispatch: `ecr`, `gar`, or `ghcr` |
| `tags` | no | `latest` | comma-separated tags; prefer an immutable version/SHA |
| `platforms` | no | `linux/amd64,linux/arm64` | comma-separated buildx target platforms |
| `dockerfile` | no | `Dockerfile` | Dockerfile path, repo-root-relative |
| `build-context` | no | `.` | build context dir, repo-root-relative |
| `cache-backend` | no | `local` | BuildKit cache: `local`, `ecr`, `gar`, or `ghcr` |
| `cache-ref` | no | _(empty)_ | registry cache ref for a non-local backend |
| `build-args` | no | _(empty)_ | `KEY=VAL,KEY=VAL` forwarded as `--build-arg` |
| `ghcr-username` | no | _(empty)_ | docker-login user for a fine-grained ghcr token |
| `token-secret` | no | `GITHUB_TOKEN` | sparkwing secret with a registry token; used for `ghcr` |

## Notes

- Tags are the one knob a release publisher should set. The default `latest`
  keeps a scaffolded run green, but a release artifact wants an immutable tag
  (a version or commit SHA) so a published image is reproducible; pass
  `--param tags=1.4.2` (optionally `1.4.2,latest`).
- A multi-arch `buildx --push` needs a builder that can target every listed
  platform. For cross-architecture builds that means QEMU emulation plus a
  non-docker-driver `docker buildx` builder instance (`docker buildx create
  --driver docker-container --use`). Building only the native architecture
  (drop the foreign entries from `platforms`) still needs a docker-container
  builder for a multi-platform `--push`.
- The `local` cache backend writes to a `.buildx-cache` directory. When the
  build context is the repo root (the default `.`), add `.buildx-cache` to
  `.dockerignore` so a `COPY . .` does not sweep the cache into the image.
  For a cache shared across runners, set `cache-backend` to `ecr`, `gar`, or
  `ghcr` and point `cache-ref` at a registry ref.
- `ecr` and `gar` authenticate through the cloud CLI (`aws`, `gcloud`) using
  ambient credentials, so `token-secret` stays unused for them. Only `ghcr`
  reads `token-secret`; `ghcr-username` only matters for a fine-grained
  GitHub token (a classic token works with the default placeholder).
- Both blocks honor `SPARKWING_DRY_RUN`: set it and each step echoes the
  exact `docker` / `aws` / `gcloud` argv instead of authenticating, building,
  or pushing. Use that to inspect what a live run would invoke without a
  registry or a builder.
- To validate that an image builds and its container starts before pushing,
  use `docker-build-smoketest` first. To push AND roll the image out to a
  cluster, use a docker-deploy template (`docker-deploy-ecr-eks` for
  AWS/EKS, `docker-deploy-gar-gke` for GCP/GKE) instead of this one.
