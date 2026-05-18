# docker-deploy-ecr-eks

Build a Docker image, push to ECR, and deploy via gitops to EKS. The
canonical pattern from consumer apps.

The rendered pipeline:

1. (Optional) runs `TestCmd` -- defaults to `go test ./...`. Set empty
   to skip tests entirely (e.g. when a CI gate already covered them).
2. Builds the Docker image from `./Dockerfile`.
3. Pushes with multi-tag (sha + branch + prod) to the configured ECR
   plus any local-kind registry detected at run time.
4. Bumps the gitops repo's kustomize image tag, commits, pushes, and
   triggers an ArgoCD sync.

Heavy lifting lives in `sparks-core/pipelines.DockerDeploy` --
sparkwing's runner detects the local-vs-cluster context and re-routes
the deploy step accordingly (kind-kustomize on local kind, gitops +
ArgoCD on real clusters).

## When to use

You're deploying a server-side application (Go service, Node API,
Ruby app, etc.) to EKS via ArgoCD-managed gitops. The image is built
from a top-level Dockerfile and the kustomize tree lives in a
separate gitops repo.

## When NOT to use

- You're on GCP -- use `docker-deploy-gar-gke`.
- Your deploy doesn't involve gitops (raw `kubectl apply`, helm
  upgrade, etc.) -- start from `pipeline new --template build-test-deploy`
  and roll your own deploy step.
- You only need static assets -- use `static-deploy-s3-cloudfront`.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `image` | yes | - | Image name |
| `ecr` | yes | - | ECR registry URL |
| `gitops-repo` | yes | - | SSH URL of gitops repo |
| `gitops-path` | yes | - | Path within gitops repo (kustomize root) |
| `app-name` | yes | - | ArgoCD application name |
| `namespace` | yes | - | Kubernetes namespace |
| `pipeline-name` | no | `build-test-deploy` | Verb users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command (empty disables) |

## After rendering

If your repo is multi-language, swap `TestCmd` to the right invocation
(`bundle exec rspec`, `npm test`, etc.). For multi-app monorepos that
build several images, look at the consumer app's `BuildDeploy` for a pattern that
fans out per-app build nodes in parallel before a single deploy step.
