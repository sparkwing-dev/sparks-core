# cloud-functions-deploy

Deploy a 2nd-gen Google Cloud Function from source with
`gcloud functions deploy --gen2`. Google's buildpacks build and deploy the
function server-side, so there is no Dockerfile to own and no local Docker
build. The trigger param selects an HTTP endpoint, a Pub/Sub topic, or a
GCS bucket; for HTTP triggers a post-deploy probe verifies the function URL.
Runtimes are polyglot: node, python, go, java.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`gcp`, `probe`, `step`) into a single `deploy` node with
a `Verify` probe, so you can see and edit the orchestration. The blocks do
the work; the scaffolded file is just the shape.

## When to use

- You ship a single event or HTTP handler as a 2nd-gen Cloud Function
  rather than a full service, and you want Google to build and deploy it
  from source.
- Your handler is triggered by an HTTP request, a Pub/Sub topic, or a GCS
  bucket event.

## When not to use

- You are deploying a multi-route HTTP service, not a single handler: use
  `cloudrun-deploy-source` (still source-built by Cloud Build) or
  `docker-deploy-gar-cloudrun` (build and push the exact image to Artifact
  Registry first).
- You want the built image published to Artifact Registry so you can
  unit-test or scan it before it goes live: use `docker-deploy-gar-cloudrun`.
- You are on AWS: the source-function twin is `lambda-deploy` with
  `package-type=zip`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `function` | yes | | Cloud Function name |
| `runtime` | yes | | Language runtime (e.g. `nodejs20`, `python312`, `go122`) |
| `entry-point` | yes | | Exported handler name, via `--entry-point` |
| `region` | yes | | Function region (e.g. `us-west1`) |
| `project` | yes | | GCP project ID |
| `trigger` | no | `http` | `http`, `topic:NAME`, or `bucket:NAME` |
| `source-dir` | no | `.` | Source directory passed to `--source` |
| `allow-unauthenticated` | no | `true` | HTTP triggers: public function; empty for private |
| `health-path` | no | `/` | HTTP triggers: path appended to the function URL for the probe |
| `pipeline-name` | no | `deploy` | Name users type after `sparkwing run` |

## Triggers

The `trigger` param maps to the gcloud trigger flags at run time:

- `http` deploys an HTTP function (`--trigger-http`), and adds
  `--allow-unauthenticated` when `allow-unauthenticated` is non-empty. Only
  this kind resolves a URL and runs the health probe.
- `topic:NAME` deploys a Pub/Sub function (`--trigger-topic NAME`).
- `bucket:NAME` deploys a GCS function (`--trigger-bucket NAME`).

For a Pub/Sub or GCS trigger there is no HTTP endpoint, so the `verify` node
skips the probe and the pipeline is a single deploy node.

## After rendering

- The `deploy` node runs `gcloud functions deploy <function> --gen2
  --runtime <runtime> --entry-point <entry-point> --source <source-dir>`.
  Point `source-dir` at the directory holding the handler source and its
  dependency manifest (a `package.json`, a `requirements.txt`, a
  `go.mod`, ...).
- For an HTTP trigger the deploy then resolves the function URL with
  `gcloud functions describe` and the `verify` node probes
  `<function-url><health-path>` for any 2xx. Tighten it in the `verify`
  method with `.ExpectStatus(200)` or `.ExpectJSON("status", "ok")` if your
  health endpoint returns structured output.
- To pass runtime settings gcloud supports (memory, timeout, env vars,
  min/max instances, a service account), add the flags to
  `functionDeployArgs` -- for example `--set-env-vars KEY=VAL`,
  `--memory 512Mi`, or `--service-account <sa>`.

## Rollback

Cloud Functions has no traffic-shifting rollback primitive: a 2nd-gen
function serves whatever revision was last deployed. To recover a bad
deploy, redeploy from the prior good source revision (check out the previous
commit and run the pipeline again), or roll the source back and let your CD
re-run this pipeline. That is why the generated `Plan()` has no `OnFailure`
node -- there is nothing to shift traffic back to.

## Dry run

The deploy honors `SPARKWING_DRY_RUN`. With that variable set, the deploy
echoes the exact `gcloud functions deploy` argv it would run and returns
success without touching GCP; it resolves no URL, so the probe is skipped.
That makes a fresh scaffold run green on a laptop with no credentials:

```sh
SPARKWING_DRY_RUN=1 sparkwing run deploy
```

Unset `SPARKWING_DRY_RUN` (and authenticate gcloud) for a real deploy.

## Credentials

The runner needs the `gcloud` CLI on PATH. Project selection and
service-account impersonation follow the `gcp` block conventions: the
`project` param wins, otherwise `GOOGLE_CLOUD_PROJECT` /
`CLOUDSDK_CORE_PROJECT` are consulted, and
`CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` adds an impersonation target.
In-cluster runners use Workload Identity from the metadata server
automatically.
