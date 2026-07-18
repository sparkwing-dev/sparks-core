# docker-deploy-gar-cloudrun

Test, build a Docker image, push it to Google Artifact Registry (GAR), and
deploy the image to a Cloud Run service. A post-deploy HTTP probe verifies
the new revision at the URL the deploy returns; a definitively-unhealthy
result shifts traffic back to the prior revision. This is the flagship GCP
serverless-container deploy.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`gcp`, `cloudrun`, `probe`) into a
`test -> build -> push -> deploy` DAG with a `Verify` probe and an
`OnFailure` rollback, so you can see and edit the orchestration. The blocks
do the work; the scaffolded file is just the shape.

The rendered pipeline:

1. (Optional) runs `test-cmd` -- defaults to `go test ./...`. Set it empty
   to skip, or swap in `npm test` / `pytest` / `cargo test` for another
   stack.
2. Builds the Docker image from `./Dockerfile` with a fresh timestamp tag.
3. Authenticates docker with GAR via `gcloud auth configure-docker`, tags
   the image for the registry, and pushes it.
4. Deploys the pushed image to Cloud Run with `gcloud run deploy --image`,
   then probes the returned service URL and rolls traffic back on a
   definitive failure.

## When to use

- You are on GCP and want a managed, serverless Cloud Run service (no
  cluster to run).
- You build and test the exact image in CI and want it pushed to GAR as a
  reproducible, scannable artifact before it goes live.
- You want a deploy that shifts traffic back to the previous revision when a
  post-deploy health check fails.

## When not to use

- You do not want to own a Dockerfile or run an image build in CI -- let
  Cloud Build's buildpacks build from source: use `cloudrun-deploy-source`.
- You run your own GKE cluster and apply k8s manifests instead of managed
  Cloud Run: use `gke-deploy-gar-kubectl`.
- You are on AWS: use `container-deploy-ecs-fargate`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `service` | yes | | Cloud Run service name (e.g. `api`) |
| `image` | yes | | Image name (e.g. `my-service`) |
| `gar` | yes | | GAR registry URL (e.g. `us-west1-docker.pkg.dev/my-project/my-repo`) |
| `region` | yes | | Cloud Run region (e.g. `us-west1`) |
| `project` | yes | | GCP project ID |
| `port` | no | `8080` | Container port the service listens on (set `3000` for a Next.js standalone server) |
| `health-path` | no | `/` | Path appended to the service URL for the post-deploy probe |
| `allow-unauthenticated` | no | `true` | Public service; set empty for a private (authenticated) one |
| `env` | no | | Runtime env vars as `KEY=VAL,KEY=VAL`, via `--set-env-vars` |
| `pipeline-name` | no | `build-test-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Pre-build test command (empty disables the test node) |

## Next.js and other stacks

The template is language-agnostic: it builds whatever your `Dockerfile`
describes. For a server-rendered Next.js app built with `output: standalone`,
set `test-cmd=npm test`, point the `Dockerfile` at your Node build, and set
`port=3000` so Cloud Run routes to the standalone server's port.

## After rendering

- The `build` node runs `docker build -f Dockerfile .`. Adjust the
  Dockerfile path or build context in the `build` method if your image lives
  elsewhere.
- The image tag is a UTC timestamp computed once in `build` and reused by
  `push` and `deploy`. Swap it for `sparkwingDocker.ComputeTags` if you want
  git-SHA-based tags.
- The probe accepts any 2xx at `<service-url><health-path>`. Tighten it in
  the `verify` method with `.ExpectStatus(200)` or `.ExpectJSON("status",
  "ok")` if your health endpoint returns structured output.
- Rollback shifts all traffic back to the prior ready revision with
  `gcloud run services update-traffic`. Swap the `OnFailure` body for your
  own recovery if a plain traffic shift is not enough.

## Health probe and rollback

The `verify` method probes the service URL the deploy returns, and the
`rollback` method distinguishes two failure modes so it does not revert a
healthy deploy:

- A **definitive unhealthy** result (a non-2xx status) shifts traffic back
  to the prior revision.
- An **indeterminate** result (`probe.Indeterminate` -- the check timed out,
  could not connect, or hit an auth error) is not evidence the new revision
  is bad, so the rollback surfaces the error and leaves the new revision in
  place for you to investigate.

## Dry run

The build runs for real (so a broken Dockerfile still fails the pipeline),
but every cloud mutation -- the GAR auth, the image push, and the Cloud Run
deploy and rollback -- honors `SPARKWING_DRY_RUN`. With that variable set,
those steps echo the exact command argv they would run and return success
without touching GCP; the deploy resolves no URL, so the probe is skipped.
That makes a fresh scaffold runnable green on a laptop with a Docker daemon
and no cloud credentials:

```sh
SPARKWING_DRY_RUN=1 sparkwing run build-test-deploy
```

Unset `SPARKWING_DRY_RUN` (and authenticate gcloud) for a real deploy.

## Credentials

The runner needs `docker` and the `gcloud` CLI on PATH. Project selection
and service-account impersonation follow the `gcp` block conventions: the
`project` param wins, otherwise `GOOGLE_CLOUD_PROJECT` /
`CLOUDSDK_CORE_PROJECT` are consulted, and
`CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` adds an impersonation target to
every `gcloud` call. In-cluster runners use Workload Identity from the
metadata server automatically.
