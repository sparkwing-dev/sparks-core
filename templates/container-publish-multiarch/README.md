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
  --param registry-kind=ecr
```

GAR:

```sh
sparkwing pipeline new --name publish-image --template container-publish-multiarch \
  --param image=myapp \
  --param registry=us-west1-docker.pkg.dev/my-project/my-repo \
  --param registry-kind=gar
```

GHCR (token login):

```sh
sparkwing pipeline new --name publish-image --template container-publish-multiarch \
  --param image=org/myapp \
  --param registry=ghcr.io/org \
  --param registry-kind=ghcr \
  --param token-secret=GHCR_TOKEN
```

## What it does

One `publish` Job:

1. `docker.RegistryLogin` authenticates the local docker client, dispatching
   on `registry-kind`: `ecr` runs `aws ecr get-login-password | docker login`,
   `gar` runs `gcloud auth configure-docker`, `ghcr` pipes the `token-secret`
   value into `docker login --password-stdin`.
2. `docker.BuildxPublish` runs `docker buildx build --platform <platforms>
   --push`, building every requested architecture and pushing one multi-arch
   manifest tagged `latest` to `<registry>/<image>`.

No smoke-test, no deploy: the artifact is the pushed manifest.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-image` | pipeline registration name |
| `image` | no | `myapp` | image name / repo path within the registry |
| `registry` | yes | | registry host / prefix to push to |
| `registry-kind` | no | `ecr` | auth dispatch: `ecr`, `gar`, or `ghcr` |
| `platforms` | no | `linux/amd64,linux/arm64` | comma-separated buildx target platforms |
| `dockerfile` | no | `Dockerfile` | Dockerfile path, repo-root-relative |
| `build-context` | no | `.` | build context dir, repo-root-relative |
| `token-secret` | no | _(empty)_ | sparkwing secret with a registry token; required for `ghcr` |

## Notes

- A multi-arch `buildx --push` needs a builder that can target every listed
  platform. For cross-architecture builds that means QEMU emulation plus a
  `docker buildx` builder instance (`docker buildx create --use`). Building
  only the native architecture (drop the foreign entries from `platforms`)
  needs neither.
- `ecr` and `gar` authenticate through the cloud CLI (`aws`, `gcloud`) using
  ambient credentials, so `token-secret` stays empty for them. Only `ghcr`
  reads `token-secret`.
- Both blocks honor `SPARKWING_DRY_RUN`: set it and each step echoes the
  exact `docker` / `aws` / `gcloud` argv instead of authenticating, building,
  or pushing. Use that to inspect what a live run would invoke without a
  registry or a builder.
- To validate that an image builds and its container starts before pushing,
  use `docker-build-smoketest` first. To push AND roll the image out to a
  cluster, use a docker-deploy template (`docker-deploy-ecr-eks` for
  AWS/EKS, `docker-deploy-gar-gke` for GCP/GKE) instead of this one.
- Change the published tag by editing the rendered `docker.BuildxConfig`
  (add a `Tags` slice); the default is a single `latest`.
