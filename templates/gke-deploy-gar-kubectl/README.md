# gke-deploy-gar-kubectl

Test, build a Docker image to Google Artifact Registry, fetch GKE cluster
credentials, and deploy to GKE by applying the repo's own manifests with
`kubectl apply`. A post-deploy HTTP probe verifies the new revision; an
unhealthy result triggers an automatic `kubectl rollout undo`.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`gcp`, `docker`, `kube`, `probe`, `rollback`) into a
`test -> build -> deploy` DAG directly, so you can see and edit the
orchestration. The blocks do the work; the scaffolded file is just the
shape. It is the GCP twin of `go-test-build-deploy-k8s` (which targets
ECR/EKS on AWS): the only GCP-specific additions are `gcloud auth
configure-docker` for the GAR push and `gcloud container clusters
get-credentials` for the kubeconfig bootstrap.

The rendered pipeline:

1. (Optional) runs `test-cmd` -- defaults to `go test ./...`.
2. Authenticates docker against the GAR host, builds the image from
   `./Dockerfile`, and pushes it to `gar`.
3. Fetches GKE credentials for `cluster` in `region` of `project`,
   applies every manifest under `k8s-path`, and rolls `deploy/<app-name>`
   to the freshly built image.
4. Probes `health-url`; a definitively-unhealthy result rolls the
   deployment back with `kubectl rollout undo`.

## When to use

- You're on GCP, deploying to a GKE cluster.
- Your repo owns its Kubernetes YAML and you apply it directly with
  `kubectl` -- no gitops repo, no ArgoCD, no kustomize indirection.
- You want a deploy that rolls itself back when a health check fails.

## When not to use

- You're on AWS: use `go-test-build-deploy-k8s` (the ECR/EKS twin).
- You deploy through a gitops repo + ArgoCD: use `docker-deploy-gar-gke`.
- You don't run a GKE cluster and want managed serverless containers: use
  `docker-deploy-gar-cloudrun` (build and push the image yourself) or
  `cloudrun-deploy-source` (let Cloud Build build from source).

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `my-service`) |
| `gar` | yes | | GAR registry URL (e.g. `us-west1-docker.pkg.dev/my-project/my-repo`) |
| `cluster` | yes | | GKE cluster name (get-credentials target) |
| `region` | yes | | Cluster location: region or zone (e.g. `us-west1` or `us-west1-a`) |
| `project` | yes | | GCP project ID |
| `namespace` | yes | | Kubernetes namespace |
| `app-name` | yes | | Deployment name; waits on / rolls back `deploy/<app-name>` |
| `health-url` | yes | | URL the post-deploy probe checks for a 2xx |
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

## Kubernetes manifests (you supply these)

The `deploy` node runs `kubectl apply -f <k8s-path>` and then `kubectl set
image deploy/<app-name> <app-name>=<built-image>`. So your `k8s-path`
directory must contain a Deployment (plus a Service) and **three names
must agree** -- a mismatch compiles fine and only fails at deploy time:

- the Deployment's `metadata.name` and its container `name` must both be
  `<app-name>` (the pipeline rolls `deploy/<app-name>` and sets the image
  on the container named `<app-name>`);
- the Service that backs `health-url` must resolve to those pods.

A minimal starter (`k8s/`), with `<app-name>` = `myapp`, namespace
`myapp`, container port 8080:

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: myapp, namespace: myapp, labels: { app: myapp } }
spec:
  replicas: 1
  selector: { matchLabels: { app: myapp } }
  template:
    metadata: { labels: { app: myapp } }
    spec:
      containers:
        - name: myapp            # MUST equal app-name
          image: myapp:latest    # set-image overwrites the tag each deploy
          ports: [{ containerPort: 8080 }]
          readinessProbe: { httpGet: { path: /health, port: 8080 } }
---
# k8s/service.yaml
apiVersion: v1
kind: Service
metadata: { name: myapp, namespace: myapp }
spec:
  selector: { app: myapp }
  ports: [{ port: 8080, targetPort: 8080 }]
```

The post-deploy probe runs from wherever the pipeline node runs: in-cluster
that's the pod network (use the Service DNS, `myapp.myapp.svc:8080`); from a
laptop it's the host (use an Ingress/LoadBalancer or port-forward URL). Set
`health-url` accordingly.

## GCP auth and kube context

The build node registers gcloud as a docker credential helper for the GAR
host with `gcloud auth configure-docker`, so the local gcloud identity
authenticates the push. The deploy node runs `gcloud container clusters
get-credentials` to write the kubeconfig context that the `kube` block then
targets.

Every `kubectl` call resolves an explicit context and **fails closed** -- it
will not fall through to whatever context is currently active in your
kubeconfig. `get-credentials` writes a context named
`gke_<project>_<region>_<cluster>`; point the kube block at it with
`SPARKWING_KUBE_CONTEXT=<that context>`, or set it on the individual
`ApplyConfig` / `SetImageConfig` / rollback `Config` if you prefer. In-cluster
runners use the pod service account automatically.

Under `SPARKWING_DRY_RUN` the `gcloud` mutations (`configure-docker`,
`get-credentials`) and the `kubectl` mutations echo their argv instead of
executing, so you can preview the exact commands without reaching the
cluster. The docker build still runs for real.

The runner needs `gcloud`, `docker`, and `kubectl` on PATH.
