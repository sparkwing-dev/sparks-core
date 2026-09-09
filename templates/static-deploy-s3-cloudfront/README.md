# static-deploy-s3-cloudfront

Build a Next.js (or any static-output) app and ship it to S3 +
CloudFront. The rendered pipeline:

1. Runs `npm ci && npm run build` in a Node container (the default
   `container` strategy). Set `nextjs-strategy` to `host` to build
   directly on the runner with `npm install && npm run build` and no
   Docker.
2. Verifies HTML chunk references resolve before touching S3 (catches
   stale-build / export-mode-not-engaged failures).
3. Syncs `out/` to the target S3 bucket with `--delete`.
4. Issues a CloudFront cache invalidation for `/*`.

It uses `pipelines.StaticDeploy` + `pipelines.NextJSBuild` from
sparks-core, so the heavy lifting (Docker build container, env-var
prefix forwarding, cache volumes) lives in one tested place.

## When to use

You have a Next.js (or other static-output) site and you want it on S3
behind CloudFront with cache invalidation. You're OK with `npm ci &&
npm run build` as the build command; if you need a different
toolchain, swap `BuildCmd` after rendering and set `out-dir` to its
output directory.

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
| `aws-profile` | no | `""` | AWS profile for the aws CLI; empty resolves via AWS_PROFILE, IRSA, or the default profile |
| `url` | no | `https://example.com` | Deployed URL (logged on success) |
| `out-dir` | no | `out` | Build output directory synced to S3 |
| `node-image` | no | `node:22-alpine` | Node image for the container strategy |
| `nextjs-strategy` | no | `container` | Build strategy: `container` or `host` |
| `pipeline-name` | no | `deploy` | Verb users type after `sparkwing run` |
| `site-cache` | no | - | Per-site `.next/cache` Docker volume name |

## After rendering

Edit the rendered `.sparkwing/jobs/<name>.go` to:

- Add a `BuildEnvPrefixes` entry for any `NEXT_PUBLIC_*` vars your
  site reads (the default forwards `NEXT_PUBLIC_` and `NEXT_EXPORT`).
- Add `Excludes` if a separate pipeline ships artifacts to the same
  bucket (e.g. `releases/*` for a CLI binary tarball).

`StaticDeploy.Run` performs build, validation and deployment in order inside
one job. Use `StaticDeploy.BuildOnly` when a build-and-check pipeline should
stop before deployment. If you split these into separate jobs, declare the
dependency with `.Needs(build)` and transfer the built files as artifacts;
ordering alone does not give jobs a shared filesystem.

A memoization key is evaluated before its job runs. Avoid keying the combined
build-and-deploy job on `out/`, because that output has not been built yet.
Memoize a separate deployment step only after its build output is available,
and include the output digest, destination and deployment configuration in
the key. Consider whether replay should skip deployment when remote state
has changed independently of the source.
