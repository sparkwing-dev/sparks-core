# docker-deploy-gar-gke

Build a Docker image, push to Google Artifact Registry, and deploy via
gitops to GKE. GCP mirror of `docker-deploy-ecr-eks`.

The rendered pipeline:

1. (Optional) runs `TestCmd` -- defaults to `go test ./...`.
2. Builds the Docker image from `./Dockerfile`.
3. Authenticates with GAR via `gcloud auth configure-docker` and
   pushes with multi-tag (sha + branch + prod).
4. Bumps the gitops repo's kustomize image tag, commits, pushes, and
   triggers an ArgoCD sync.

## When to use

You're on GCP, deploying to GKE via ArgoCD-managed gitops. Multi-cloud
parity if a sibling project uses `docker-deploy-ecr-eks`.

## When NOT to use

- You're on AWS -- use `docker-deploy-ecr-eks`.
- Your deploy doesn't involve gitops -- roll your own deploy step.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `image` | yes | - | Image name |
| `gar` | yes | - | GAR registry URL |
| `gitops-repo` | yes | - | SSH URL of gitops repo |
| `gitops-path` | yes | - | Path within gitops repo |
| `app-name` | yes | - | ArgoCD application name |
| `namespace` | yes | - | Kubernetes namespace |
| `pipeline-name` | no | `build-test-deploy` | Verb users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command (empty disables) |

## Note on sparks-core coverage

sparks-core's `DockerDeploy` today is ECR-shaped (e.g. `ECR` field
name, registry resolution honors AWS-specific env hints). This
template uses raw `sparkwing.Bash` calls until a `sparks-core/gar`
+ GKE-aware `DockerDeploy` lands. The shape mirrors
`docker-deploy-ecr-eks` so a future port to a unified
`pipelines.DockerDeploy` should be a one-line struct field swap.
