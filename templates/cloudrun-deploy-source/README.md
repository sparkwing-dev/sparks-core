# cloudrun-deploy-source

Deploy a service to Cloud Run directly from source with
`gcloud run deploy --source`. Cloud Build's buildpacks detect the language
and build the container server-side, so there is no Dockerfile to own and
no local Docker build. A post-deploy HTTP probe verifies the new revision
at the URL the deploy returns; a definitively-unhealthy result shifts
traffic back to the prior revision.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`cloudrun`, `probe`) into a single `deploy` node with a
`Verify` probe and an `OnFailure` rollback, so you can see and edit the
orchestration. The blocks do the work; the scaffolded file is just the
shape.

## When to use

- You want to ship a Cloud Run service without owning a Dockerfile or
  running an image build in CI -- let Cloud Build's buildpacks build it.
- You want a deploy that shifts traffic back to the previous revision when
  a post-deploy health check fails.

## When not to use

- You want to build and test the exact image in CI and push it to
  Artifact Registry (a reproducible, scannable artifact) before it goes
  live: use `docker-deploy-gar-cloudrun`. Source deploys build the image
  server-side, so there is no local image to unit-test or scan first.
- You are deploying a single event handler rather than a full HTTP
  service: use `cloud-functions-deploy`.
- You run your own GKE cluster instead of managed Cloud Run: use
  `gke-deploy-gar-kubectl`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `service` | yes | | Cloud Run service name (e.g. `api`) |
| `region` | yes | | Cloud Run region (e.g. `us-west1`) |
| `project` | yes | | GCP project ID |
| `source-dir` | no | `.` | Source directory passed to `--source` |
| `health-path` | no | `/` | Path appended to the service URL for the probe |
| `allow-unauthenticated` | no | `true` | Public service; set `false` (or empty) for a private (authenticated) one |
| `env` | no | | Non-secret runtime env vars as `KEY=VAL,KEY=VAL`, via `--set-env-vars` |
| `pipeline-name` | no | `deploy` | Name users type after `sparkwing run` |

## After rendering

- The `deploy` node runs `gcloud run deploy <service> --source
  <source-dir>`. Point `source-dir` at the directory whose buildpack-
  detectable source you want built (a Go module, a `package.json`, a
  `requirements.txt`, ...).
- The probe accepts any 2xx at `<service-url><health-path>`. Tighten it in
  the `verify` method with `.ExpectStatus(200)` or `.ExpectJSON("status",
  "ok")` if your health endpoint returns structured output.
- The probe polls `.Retry(30).Interval(2 * time.Second)` (a ~60s health
  window). A service with a slow cold start that serves transient 5xx past
  that window trips a false rollback -- widen `Retry`/`Interval` in the
  `verify` method for a slow starter.
- `env` values are written in plaintext into the rendered pipeline, the
  `gcloud` argv, and the service config -- keep secrets out of it. Reference
  Secret Manager instead by adding `ExtraArgs: []string{"--set-secrets",
  "DB_URL=db-url:latest"}` to the `DeployConfig`.
- Rollback shifts all traffic back to the revision that was serving before
  the deploy (captured from the deploy result) with `gcloud run services
  update-traffic`. Swap the `OnFailure` body for your own recovery if a
  plain traffic shift is not enough.
- To ship a preview revision that does not take production traffic, set
  `NoTraffic` and a `Tag` on the `DeployConfig` and probe the returned tag
  URL before promoting.

## Health probe and rollback

The `verify` method probes the service URL the deploy returns, and the
`rollback` method distinguishes three failure modes so it only reverts
traffic when the new revision is proven bad:

- A **definitive unhealthy** result (a non-2xx status) shifts traffic back
  to the revision that was serving before the deploy.
- An **indeterminate** result (`probe.Indeterminate` -- the check timed
  out, could not connect, or hit an auth error) is not evidence the new
  revision is bad, so the rollback surfaces the error and leaves the new
  revision in place for you to investigate.
- A failure **before the probe runs** (the deploy body itself erroring --
  an unbuildable source tree, a bad flag, missing IAM) rolled nothing out,
  so the current revision is still serving. The rollback surfaces that
  error and leaves traffic untouched rather than reverting a healthy
  service off the back of a build error.

## Dry run

Every cloud mutation in this template routes through the `cloudrun` block,
which honors `SPARKWING_DRY_RUN`. With that variable set, the deploy and
any rollback echo the exact `gcloud` argv they would run and return success
without touching GCP; the deploy resolves no URL, so the probe is skipped.
That makes a fresh scaffold runnable green on a laptop with no credentials:

```sh
SPARKWING_DRY_RUN=1 sparkwing run deploy
```

Unset `SPARKWING_DRY_RUN` (and authenticate gcloud) for a real deploy.

## Credentials

The runner needs the `gcloud` CLI on PATH. Project selection and
service-account impersonation follow the `gcp` block conventions: the
`project` param wins, otherwise `GOOGLE_CLOUD_PROJECT` /
`CLOUDSDK_CORE_PROJECT` are consulted, and
`CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` adds an impersonation target to
every `gcloud` call. In-cluster runners use Workload Identity from the
metadata server automatically.
