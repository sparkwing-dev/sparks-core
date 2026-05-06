# static-deploy-s3-cloudfront

Build a Next.js (or any static-output) app and ship it to S3 +
CloudFront. The rendered pipeline:

1. Runs `npm ci && npm run build` in a Node container (host build on
   laptop runs).
2. Verifies HTML chunk references resolve before touching S3 (catches
   stale-build / export-mode-not-engaged failures).
3. Syncs `out/` to the target S3 bucket with `--delete`.
4. Issues a CloudFront cache invalidation for `/*`.

This is the simplified canonical version of patterns running today in
multiple consumer sites. It uses
`pipelines.StaticDeploy` + `pipelines.NextJSBuild` from sparks-core, so
the heavy lifting (Docker build container, env-var prefix forwarding,
cache volumes) lives in one tested place.

## When to use

You have a Next.js (or other static-output) site and you want it on S3
behind CloudFront with cache invalidation. You're OK with `npm ci &&
npm run build` as the build command; if you need a different
toolchain, swap `BuildCmd` after rendering.

## When NOT to use

- Your site needs SSR or API routes -- use `docker-deploy-ecr-eks`
  instead and run Next as a server.
- You're on GCP -- use `static-deploy-gcs-cloudcdn`.
- You only need to build (no deploy) -- use `next-build-and-push`.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `bucket` | yes | - | Target S3 bucket |
| `distribution` | yes | - | CloudFront distribution ID |
| `url` | no | `https://example.com` | Deployed URL (logged on success) |
| `pipeline-name` | no | `deploy` | Verb users type after `wing` |
| `site-cache` | no | - | Per-site `.next/cache` Docker volume name |

## After rendering

Edit the rendered `.sparkwing/jobs/<name>.go` to:

- Add a `BuildEnvPrefixes` entry for any `NEXT_PUBLIC_*` vars your
  site reads (the default forwards `NEXT_PUBLIC_` and `NEXT_EXPORT`).
- Add `Excludes` if a separate pipeline ships artifacts to the same
  bucket (e.g. `releases/*` for a CLI binary tarball).
- Add a `.Cache(...)` modifier if you want skip-on-noop behavior --
  doc-only commits won't redeploy. See the consumer app's `BuildDeploy`
  for an example.
