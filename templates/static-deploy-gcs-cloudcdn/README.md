# static-deploy-gcs-cloudcdn

Build a Next.js (or any static-output) app and ship it to GCS + Cloud
CDN. The GCP counterpart of `static-deploy-s3-cloudfront`.

The rendered pipeline:

1. Runs `npm ci && npm run build` on the host (with `NEXT_EXPORT=1`
   for the build), so the runner needs node + npm on PATH. `npm ci`
   installs the lockfile exactly, so the same commit builds the same
   dependency tree every run.
2. Syncs the output directory (`out` by default) to the target GCS
   bucket via `gsutil rsync -d`. The immutable `Cache-Control` header
   is stamped at upload time via `gsutil -h`, so only the objects this
   run writes are touched; a second pass overrides HTML to `no-cache`
   so a redeploy is served on the next request.
3. Issues a Cloud CDN cache invalidation against the configured URL
   map and waits for it to finish before logging the deployed URL.

Every stage retries with a short backoff and carries a timeout, since
each one is a network call (registry fetch, GCS sync, CDN API).

## Difference from the S3 twin

The AWS template runs the build in a Docker container via
`pipelines.NextJSBuild` (npm ci, `node:22-alpine`, shared npm + per-site
cache volumes) and cross-checks HTML chunk references before syncing.
This template keeps a plain host build and drives GCS + Cloud CDN with
`gsutil` and `gcloud` directly, so a scaffold has no sparks-core GCP
dependency. If your team grows GCP usage, the natural next step is to
lift these calls into a `sparks-core/gcs` package mirroring
`sparks-core/s3`.

## When to use

You're on GCP, you have a static site, and you want CDN-fronted
delivery.

## When NOT to use

- You're on AWS -- use `static-deploy-s3-cloudfront`.
- You need server-side rendering -- use a docker template.
- You don't have Cloud CDN provisioned yet -- this template assumes
  the URL map already exists.
- The bucket holds anything other than this site. `rsync -d` deletes
  every object not present in the build output, and the HTML cache-header
  pass runs over the whole bucket. Point this only at a bucket dedicated
  to the site.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `bucket` | yes | - | GCS bucket name |
| `url-map` | yes | - | Cloud CDN URL map for cache invalidation |
| `project` | yes | - | GCP project ID |
| `out-dir` | no | `out` | Build output directory to sync |
| `url` | no | `https://example.com` | Deployed URL (logged on success) |
| `pipeline-name` | no | `deploy` | Verb users type after `sparkwing run` |
