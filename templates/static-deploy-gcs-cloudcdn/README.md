# static-deploy-gcs-cloudcdn

Build a Next.js (or any static-output) app and ship it to GCS + Cloud
CDN. The GCP-flavored mirror of `static-deploy-s3-cloudfront`.

The rendered pipeline:

1. Runs `npm ci && npm run build` (or `npm install && npm run build`
   on laptops, via `pipelines.NextJSBuild`).
2. Syncs `out/` to the target GCS bucket via `gsutil rsync -d`.
3. Issues a Cloud CDN cache invalidation against the configured URL map.

## When to use

You're on GCP, you have a static site, and you want CDN-fronted
delivery. The shape mirrors the AWS path, so swapping clouds later
costs you a re-render rather than a rewrite.

## When NOT to use

- You're on AWS -- use `static-deploy-s3-cloudfront`.
- You need server-side rendering -- use a docker template.
- You don't have Cloud CDN provisioned yet -- this template assumes
  the URL map already exists.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `bucket` | yes | - | GCS bucket name |
| `url-map` | yes | - | Cloud CDN URL map for cache invalidation |
| `project` | yes | - | GCP project ID |
| `url` | no | `https://example.com` | Deployed URL (logged on success) |
| `pipeline-name` | no | `deploy` | Verb users type after `wing` |

## Note on sparks-core coverage

sparks-core today targets AWS for static deploy. This template uses
`sparkwing.Bash(...)` calls to gsutil + gcloud directly so it works
without a sparks-core/gcs module. If your team grows GCP usage, the
right move is to lift these calls into a `sparks-core/gcs` package
mirroring `sparks-core/s3` -- this template is a starter, not the
final shape.
