# The kind templates are removed

Modules: `templates`.

`go-test-build-deploy-k8s` and `canary-deploy-k8s` are gone. Both
branched on `SPARKWING_KIND_CLUSTER` to load the image they had just
built into a local cluster, and kind support was removed from `docker`,
`kube`, `deploy` and `rollback` in the sweep that made the caller name
the target.

Nothing failed loudly, which is why this is a removal rather than a
patch. The kind branch still compiled, so `template-verify` kept passing
both templates on every release. A pipeline scaffolded from either one
built an image, never got it to a cluster, and then deployed whatever
was already running.

## What to use instead

| Scaffolded from | Use |
|---|---|
| `go-test-build-deploy-k8s` | `gke-deploy-gar-kubectl`, swapping the GAR push and the `get-credentials` node for ECR and EKS |
| `canary-deploy-k8s` | `docker-deploy-ecr-eks` or `docker-deploy-gar-gke`, with the canary steps written into the deploy node |

`gke-deploy-gar-kubectl` is now the only template that applies the
repo's own manifests with `kubectl`. Its GCP-specific parts are two
nodes -- `gcloud auth configure-docker` and `gcloud container clusters
get-credentials` -- and the rest of the DAG is cloud agnostic.

## If you already scaffolded one

Nothing in your repo breaks. These templates generate a
`.sparkwing/jobs/<name>.go` that you own from the moment it is rendered,
and removing the template does not touch it.

What to check in that generated file:

1. Any `SPARKWING_KIND_CLUSTER` read. It selects nothing now. Delete the
   branch and keep the registry path.
2. That the registry you push to is one the cluster can pull from. The
   kind branch existed so a local cluster could read an image that never
   left the machine; without it, an image pushed nowhere reachable
   deploys as a stale tag.
3. `docker.BuildAndPush` no longer loads images into kind, so a build
   that used to end at `kind load docker-image` needs a real push
   target.

## Rendering an old template

Both are still readable in git history:

```
git show templates/v0.30.0:templates/go-test-build-deploy-k8s/pipeline.go.tmpl
git show templates/v0.30.0:templates/canary-deploy-k8s/pipeline.go.tmpl
```

Pinning `sparks-core/templates` at v0.30.0 also still renders them, at
the cost of every fix after that tag.
