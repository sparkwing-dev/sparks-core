# docker-deploy-gar-gke

Test, build a Docker image, push it to Google Artifact Registry, and
deploy to GKE through a gitops repo: bump the kustomize image tag, push,
and wait for ArgoCD to sync the new revision to Synced + Healthy. The GCP
twin of `docker-deploy-ecr-eks`.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`gcp`, `docker`, `deploy`) into a `test -> build ->
deploy` DAG directly, so you can see and edit the orchestration. The
blocks do the work; the scaffolded file is just the shape. The `deploy`
node calls the same deploy dispatcher the AWS twin uses, so a remote
target goes through gitops + ArgoCD and a local kind cluster gets the
repo's own manifests applied.

The rendered pipeline:

1. (Optional) runs `test-cmd` -- defaults to `go test ./...`.
2. Authenticates docker against the GAR host with `gcloud auth
   configure-docker <host>`, builds the image from `./Dockerfile`, and
   pushes it to `registry` tagged with a content-addressed tag (git short
   SHA plus a fileset hash), so a deployed image traces back to its build
   inputs.
3. Bumps the kustomize image tag in the gitops repo (retrying on
   concurrent pushes), commits, pushes, and kicks an ArgoCD sync, waiting
   until the application reports Synced + Healthy. Against a local kind
   cluster it applies the repo's `k8s/` manifests instead.

## Scaffold

```sh
sparkwing pipeline new --name build-test-deploy --template docker-deploy-gar-gke \
  --param image=my-service \
  --param registry=us-west1-docker.pkg.dev/my-project/my-repo \
  --param gitops-repo=git@github.com:org/gitops.git \
  --param gitops-path=apps/my-service \
  --param app-name=my-service --param namespace=my-service
```

## When to use

- You're on GCP, deploying to GKE, and an ArgoCD Application tracks a
  gitops repo you ship to by bumping a kustomization image tag.
- You want multi-cloud parity with a sibling project that uses
  `docker-deploy-ecr-eks`.

## When not to use

- You're on AWS: use `docker-deploy-ecr-eks`.
- Your repo owns its Kubernetes YAML and you apply it directly with
  `kubectl` (no gitops, no ArgoCD): use `gke-deploy-gar-kubectl`.
- You don't run a cluster and want managed serverless containers: use
  `docker-deploy-gar-cloudrun` (build and push the image yourself) or
  `cloudrun-deploy-source` (deploy from source).

## Prerequisites

- A Dockerfile at the repo root (`docker build -f Dockerfile .`).
- `gcloud` and `docker` on PATH, with gcloud authenticated to the
  project (for GAR auth and the push).
- A gitops repo with a `kustomization.yaml` at `gitops-path` whose image
  transformer references `registry/image`, plus write access to that repo
  (a `GITHUB_TOKEN` PAT or an SSH key).
- An ArgoCD Application named `app-name` tracking that repo against your
  GKE cluster.
- For a local kind run, `kubectl` on PATH and a `k8s/` kustomization in
  the repo.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `my-service`) |
| `registry` | yes | | Container registry URL to push to (e.g. `us-west1-docker.pkg.dev/proj/repo`) |
| `gitops-repo` | yes | | SSH URL of the gitops repo |
| `gitops-path` | yes | | Path within the gitops repo (kustomize root) |
| `app-name` | yes | | ArgoCD application name; the deploy waits on it reaching Synced + Healthy |
| `namespace` | yes | | Kubernetes namespace; the local-kind path targets it directly |
| `pipeline-name` | no | `build-test-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command; empty disables the test node |

## After rendering

- The `build` node runs `docker build -f Dockerfile .`. Adjust the
  Dockerfile path or build context in the `build` method if your image
  lives elsewhere.
- The `test-cmd` default is Go, but the node just runs a shell command:
  set it to `pytest`, `npm test`, `cargo test`, or empty to skip the test
  node.
- The deploy image tag is the content-addressed `DeployTag`
  (`commit-<sha>-files-<hash>`); the gitops kustomization is bumped to the
  same tag that was pushed to `registry`.

## Dry run

The build runs for real (so a broken Dockerfile still fails the
pipeline), but the GAR auth and image push honor `SPARKWING_DRY_RUN` and
echo their argv instead of touching GCP. The gitops push and ArgoCD sync
reach live infrastructure, so a full end-to-end dry run needs the gitops
repo and ArgoCD reachable.

## Credentials

The runner needs `docker` and the `gcloud` CLI on PATH. GAR auth uses the
active gcloud identity. The gitops push authenticates with a `GITHUB_TOKEN`
PAT when set, falling back to an SSH key; the ArgoCD sync uses
`SPARKWING_ARGOCD_SERVER` + `SPARKWING_ARGOCD_TOKEN`, or in-cluster
service discovery when those are unset.
