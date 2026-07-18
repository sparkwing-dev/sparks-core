# docker-deploy-ecr-eks

Build a Docker image, push to ECR, and deploy via gitops to EKS. Tests
run before the build; the image is pushed multi-tag; a gitops bump plus
ArgoCD sync rolls it out.

The rendered pipeline:

1. (Optional) runs `TestCmd` -- defaults to `go test ./...`. Set empty
   to skip tests entirely (e.g. when a CI gate already covered them).
2. Builds the Docker image from the configured Dockerfile (default
   `./Dockerfile`) and build context (default `.`).
3. Pushes with multi-tag (sha + branch + prod) to the configured
   registry plus any local-kind registry detected at run time.
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
  upgrade, etc.) -- start from `go-test-build-deploy-k8s`, which applies
  the repo's own manifests with kubectl, and edit its deploy node.
- You only need static assets -- use `static-deploy-s3-cloudfront`.

## Prerequisites

- A `Dockerfile` at the build context (the build node runs `docker
  build` against it).
- An existing ECR repository for `image`.
- An SSH deploy key granting the runner write access to the gitops repo.
- A pre-created ArgoCD application named `app-name`; the deploy bumps the
  kustomize tag and triggers its sync, it does not create the app.
- An EKS cluster with `kubectl` on PATH. Pushes authenticate via AWS
  credentials with ECR push access (`AWS_PROFILE` locally, or IRSA on an
  in-cluster runner).

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `image` | yes | - | Image name |
| `registry` | yes | - | Container registry URL to push to |
| `gitops-repo` | yes | - | SSH URL of gitops repo |
| `gitops-path` | yes | - | Path within gitops repo (kustomize root) |
| `app-name` | yes | - | ArgoCD application name |
| `namespace` | yes | - | Kubernetes namespace |
| `dockerfile` | no | `Dockerfile` | Path to the Dockerfile |
| `build-context` | no | `.` | Docker build context directory |
| `platform` | no | `` | Build target platform; set `linux/amd64` to match EKS nodes from an arm64 machine |
| `pipeline-name` | no | `build-test-deploy` | Verb users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command (empty disables) |

## After rendering

If your repo is multi-language, swap `TestCmd` to the right invocation
(`bundle exec rspec`, `npm test`, etc.). For multi-app monorepos that
build several images, implement `Plan()` on the outer struct and fan out
per-app build nodes in parallel before a single deploy node, calling into
`DockerDeploy`'s helpers from each node.

Local-kind registry auto-detection assumes a kind cluster named
`sparktest`. If your local cluster has a different name, the run still
targets `registry`; only the convenience local-registry detection is
skipped.
