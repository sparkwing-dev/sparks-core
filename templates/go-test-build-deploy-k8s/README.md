# go-test-build-deploy-k8s

Test, build a Docker image to ECR, and deploy to Kubernetes by applying
the repo's own manifests with `kubectl apply`. A post-deploy HTTP probe
verifies the new revision; an unhealthy result triggers an automatic
`kubectl rollout undo`.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`docker`, `kube`, `probe`) into a `test -> build ->
deploy` DAG directly, so you can see and edit the orchestration. The
blocks do the work; the scaffolded file is just the shape.

## When to use

- Your repo owns its Kubernetes YAML and you apply it directly with
  `kubectl` -- no gitops repo, no ArgoCD, no kustomize indirection.
- You want a deploy that rolls itself back when a health check fails.

## When not to use

- You deploy through a gitops repo + ArgoCD: use
  `go-test-migrate-deploy-argo` or `docker-deploy-ecr-eks`.
- You need database migrations as part of the deploy: use
  `go-test-migrate-deploy-argo`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `myapp`) |
| `ecr` | yes | | ECR registry URL |
| `namespace` | yes | | Kubernetes namespace |
| `health-url` | yes | | URL the post-deploy probe checks for a 2xx |
| `app-name` | yes | | Deployment name; waits on / rolls back `deploy/<app-name>` |
| `k8s-path` | no | `k8s` | Path passed to `kubectl apply -f` |
| `pipeline-name` | no | `build-test-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command; empty disables the test node |

## After rendering

- The `deploy` node applies every manifest under `k8s-path`. Point it at
  your manifests directory, or list specific files by editing
  `ApplyConfig.Paths`.
- The probe accepts any 2xx. Tighten it with `.ExpectStatus(200)` or
  `.ExpectJSON("status", "ok")` if your health endpoint returns
  structured output.
- Rollback is `kubectl rollout undo`. Swap the `OnFailure` body for your
  own recovery if a plain rollout-undo isn't enough.

The runner needs `docker` and `kubectl` on PATH.
