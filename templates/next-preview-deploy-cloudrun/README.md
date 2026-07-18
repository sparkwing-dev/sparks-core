# next-preview-deploy-cloudrun

Per-branch preview deploys for a server-rendered Next.js app on Cloud Run,
using Cloud Run's revision tags. It builds the branch image, pushes it to
Google Artifact Registry (GAR), and deploys it as a
`--tag <branch-slug> --no-traffic` revision: a stable per-branch preview URL
that never shifts production traffic. The tagged URL is printed for the pull
request. Preview environments with no extra infrastructure -- one Cloud Run
service hosts every branch's preview as its own tagged revision.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`gcp`, `cloudrun`, `probe`) into a
`test -> build -> push -> deploy` DAG with a `Verify` probe, so you can see
and edit the orchestration. The blocks do the work; the scaffolded file is
just the shape.

The rendered pipeline:

1. (Optional) runs `test-cmd` -- defaults to `npm test`. Set it empty to
   skip, or swap in another command for your stack.
2. Builds the Docker image from `./Dockerfile` with a `<branch-slug>-<timestamp>`
   tag, so each push of a branch produces a fresh image and a new preview
   revision.
3. Authenticates docker with GAR via `gcloud auth configure-docker`, tags
   the image for the registry, and pushes it.
4. Deploys the pushed image to Cloud Run with
   `gcloud run deploy --tag <branch-slug> --no-traffic`, probes the returned
   tagged preview URL, and prints it.

## When to use

- You want an ephemeral per-PR or per-branch preview environment for an SSR
  app on GCP.
- You want previews to reuse one Cloud Run service (a tagged revision per
  branch) so they cost nothing when idle and need no teardown job.
- You want the branch's preview URL to be stable and reachable without
  touching the production (traffic-serving) revision.

## When not to use

- You are deploying the production, traffic-serving revision (and want a
  traffic-shift rollback on a failed probe): use `docker-deploy-gar-cloudrun`.
- You do not run an SSR server and only ship a static export to object
  storage: static-export previews are a documented follow-up, not this
  template.
- You are on AWS: use `container-deploy-ecs-fargate`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `image` | yes | | Image name (e.g. `web`) |
| `gar` | yes | | GAR registry URL (e.g. `us-west1-docker.pkg.dev/my-project/my-repo`) |
| `service` | yes | | Base Cloud Run service; previews land as tagged revisions on it |
| `region` | yes | | GCP region (e.g. `us-west1`) |
| `project` | yes | | GCP project ID |
| `port` | no | `3000` | Container port the Next server listens on |
| `branch-ref` | no | | Branch/ref the preview slug derives from; empty reads the dispatch-time branch |
| `test-cmd` | no | `npm test` | Pre-build test command (empty disables the test node) |
| `pipeline-name` | no | `preview` | Name users type after `sparkwing run` |

## Branch to preview slug

The Cloud Run revision tag is derived from the branch name by `previewSlug`:
it lowercases the ref, collapses every run of non-alphanumeric characters to
a single hyphen, trims stray hyphens, forces a leading letter, and truncates
to a Cloud-Run-safe length. `feature/login-page` becomes `feature-login-page`,
which yields a stable preview URL like `https://feature-login-page---<service>-<hash>.<region>.run.app`.

The branch itself comes from the `branch-ref` param when set; otherwise the
pipeline reads the dispatch-time branch from the run's git snapshot
(`RunContext.Git.Branch`). Set `branch-ref` explicitly if you dispatch from a
detached checkout or want a fixed preview slug.

## No traffic, no rollback

A preview revision is deployed with `--no-traffic`, so it is reachable at its
own tagged URL but serves none of the service's production traffic. Because a
preview never affects the live service, there is deliberately **no**
`OnFailure` rollback: a failed build, push, or deploy just fails the run and
the pull request author sees a broken preview. This is the sharp difference
from `docker-deploy-gar-cloudrun`, which shifts live traffic and rolls it
back on a failed health check.

## After rendering

- The `build` node runs `docker build -f Dockerfile .`. Point it at your
  Next.js `Dockerfile` (build with `output: standalone`) and adjust `port`
  to the standalone server's port (the default is `3000`).
- The image tag is `<branch-slug>-<UTC-timestamp>`, computed once in `build`
  and reused by `push` and `deploy`. Swap it for a git-SHA-based tag if you
  prefer.
- The probe accepts any 2xx at the tagged preview URL. Tighten it in the
  `verify` method with `.ExpectStatus(200)` or `.ExpectJSON("status", "ok")`
  if your health endpoint returns structured output.
- Previews are cheap to leave in place (an idle tagged revision scales to
  zero), but you can tear one down on PR close with `cloudrun.RemoveTag`
  against the same service and slug. Add a teardown step or a separate
  pipeline if you want automatic cleanup.

## Dry run

The build runs for real (so a broken Dockerfile still fails the pipeline),
but every cloud mutation -- the GAR auth, the image push, and the Cloud Run
deploy -- honors `SPARKWING_DRY_RUN`. With that variable set, those steps
echo the exact command argv they would run and return success without
touching GCP; the deploy resolves no URL, so the probe is skipped:

```sh
SPARKWING_DRY_RUN=1 sparkwing run preview
```

Unset `SPARKWING_DRY_RUN` (and authenticate gcloud) for a real preview
deploy.

## Credentials

The runner needs `docker` and the `gcloud` CLI on PATH. Project selection and
service-account impersonation follow the `gcp` block conventions: the
`project` param wins, otherwise `GOOGLE_CLOUD_PROJECT` /
`CLOUDSDK_CORE_PROJECT` are consulted, and
`CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT` adds an impersonation target to
every `gcloud` call. In-cluster runners use Workload Identity from the
metadata server automatically.
