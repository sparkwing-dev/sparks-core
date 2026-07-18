# canary-deploy-k8s

Progressive canary rollout on Kubernetes. Build the image, deploy a small
canary alongside the stable Deployment, probe the canary's own endpoint,
then promote the stable Deployment to the new image on success or delete
the canary (leaving stable untouched) on failure.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`docker`, `kube`, `probe`) into a `build -> canary ->
probe -> {promote|abort}` DAG directly, so you can see and edit the
control flow. The blocks do the work; the scaffolded file is just the
shape.

The difference from a plain rolling deploy is *when* verification
happens. A rolling deploy flips the whole Deployment and rolls back
*after* a failed probe; this template proves the new image on a canary
slice *before* it ever touches the stable Deployment, so a bad revision
is caught without disturbing what is serving traffic.

## When to use

- A bad revision must be caught on a slice of traffic before it reaches
  everyone, and you want verification to gate promotion rather than
  trigger a revert.
- You own a canary Deployment+Service manifest in the repo (or are happy
  to add one) that runs the same app under a separate name.

## When not to use

- You want the whole Deployment flipped at once and rolled back on a
  failed health check: use `go-test-build-deploy-k8s` (kubectl) or
  `go-test-migrate-deploy-argo` (gitops + ArgoCD).
- You want an instant all-or-nothing selector cutover with a warm standby
  to fall back to: that is blue/green, a documentable variant of this
  template that swaps the promote step for a Service selector flip.
- You need database migrations as part of the rollout: use
  `go-test-migrate-deploy-argo`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name to build and roll out (e.g. `myapp`) |
| `registry` | no | | Registry URL to push to (ECR/GAR); empty resolves kind or `SPARKWING_REGISTRY` |
| `namespace` | yes | | Namespace for both the canary and the stable Deployment |
| `container` | yes | | Container name retagged on the canary and on promote |
| `stable-deployment` | yes | | Stable rollout target promoted on success (e.g. `deploy/myapp`) |
| `canary-manifest` | no | `k8s/canary.yaml` | Canary Deployment+Service manifest applied and deleted |
| `canary-deployment` | yes | | Canary Deployment waited on and cleaned up (e.g. `deploy/myapp-canary`) |
| `canary-health-url` | yes | | URL the post-deploy probe checks for a 2xx before promoting |
| `pipeline-name` | no | `canary-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command; empty disables the test node |

## What runs

1. **test** (optional) runs `test-cmd`. Elided when `test-cmd` is empty.
2. **build** builds the image and pushes it (or `kind load`s it) to the
   resolved registry.
3. **canary** applies `canary-manifest` and pins the canary Deployment to
   the freshly built image, then the node's `Verify` probe hits
   `canary-health-url`. A definitive failure fires the **abort** handler,
   which deletes the canary and leaves the stable Deployment untouched. A
   probe that could not even determine health (unreachable, timeout,
   auth) is *not* treated as a bad revision: the canary is left running
   for investigation and the error is surfaced.
4. **promote** runs only when the canary probe passed. It points the
   stable Deployment at the same image, waits for its rollout, and tears
   the canary down.

## After rendering

- The **canary** node applies `canary-manifest`, then rolls the built
  image onto `canary-deployment`. Point the probe at the canary's own
  Service, not the stable one, so you are checking the new revision.
- The probe accepts any 2xx. Tighten it with `.ExpectStatus(200)` or
  `.ExpectJSON("status", "ok")` if your health endpoint returns
  structured output, and raise `.Retry(...)` for a slow starter.
- To widen the canary slice before probing (more than one replica), add a
  `kube.Scale` call in the `canary` body against `canary-deployment`.
- Promotion is a plain `kubectl set image` on the stable Deployment. Swap
  the `promote` body if you gate promotion behind a manual approval or a
  longer soak.

## Kubernetes manifests (you supply these)

The **canary** node runs `kubectl apply -f <canary-manifest>` and then
`kubectl set image <canary-deployment> <container>=<built-image>`, so the
manifest must define a canary Deployment (plus a Service the probe can
reach) and **the names must agree** with the parameters -- a mismatch
compiles fine and only fails at deploy time:

- the canary Deployment's `metadata.name` must be the bare name behind
  `canary-deployment` (the pipeline rolls `deploy/<name>`), and its
  container `name` must equal `container`;
- the stable Deployment behind `stable-deployment` must already exist and
  use the same container `name`, since promote sets the image on it;
- the Service that backs `canary-health-url` must select the canary pods.

A minimal canary manifest (`k8s/canary.yaml`), with `canary-deployment` =
`deploy/myapp-canary`, `container` = `myapp`, namespace `myapp`, container
port 8080:

```yaml
# k8s/canary.yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: myapp-canary, namespace: myapp, labels: { app: myapp, track: canary } }
spec:
  replicas: 1
  selector: { matchLabels: { app: myapp, track: canary } }
  template:
    metadata: { labels: { app: myapp, track: canary } }
    spec:
      containers:
        - name: myapp             # MUST equal the container param
          image: myapp:latest     # set-image overwrites the tag each run
          imagePullPolicy: IfNotPresent
          ports: [{ containerPort: 8080 }]
          readinessProbe: { httpGet: { path: /health, port: 8080 } }
---
# k8s/canary-service.yaml (bundled into the same file is fine)
apiVersion: v1
kind: Service
metadata: { name: myapp-canary, namespace: myapp }
spec:
  selector: { app: myapp, track: canary }
  ports: [{ port: 8080, targetPort: 8080 }]
```

The stable Deployment (named `myapp`, without the `track: canary` label)
lives in your existing manifests and is not applied here; promote only
sets its image.

The probe runs from wherever the pipeline node runs: in-cluster that is
the pod network (use the Service DNS, `myapp-canary.myapp.svc:8080`); from
a laptop/kind it is the host (use a NodePort or port-forward URL). Set
`canary-health-url` accordingly.

## Kube context

Every `kubectl` call resolves an explicit context and **fails closed** --
it will not fall through to whatever context is currently active in your
kubeconfig (which might be the wrong cluster). Configure the target once:

- `SPARKWING_KUBE_CONTEXT=<context>` -- the cluster to deploy to, or
- `SPARKWING_KIND_CLUSTER=<name>` -- resolves to `kind-<name>` for local
  kind runs (and routes the build to `kind load`).

In-cluster runners use the pod service account automatically. As a last
resort, `SPARKWING_KUBE_ALLOW_CURRENT=1` opts into the current context.

The runner needs `docker` and `kubectl` on PATH. On EKS versus GKE only
the `registry` value differs; the rollout is pure `kubectl`.
