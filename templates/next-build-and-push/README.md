# next-build-and-push

Build a Next.js static export and upload the resulting `out/` as a
tarball to an artifact store. **No deploy step.** Useful for split
build/promote workflows where the deploy is a manually-gated separate
pipeline.

The rendered pipeline:

1. Runs `npm install && npm run build` with `NEXT_EXPORT=1`.
2. Tarballs `out/` to `out.tar.gz`.
3. Uploads the tarball to `s3://<bucket>/<prefix>/<sha>.tar.gz`.

## When to use

- Your deploy gate is approval-bound and the build should run
  unconditionally on merge.
- You want cached build artifacts retained for forensics or rollback.
- A downstream cross-repo trigger pulls the artifact and deploys.

## When NOT to use

- The deploy should be automatic -- use `static-deploy-s3-cloudfront`
  or its GCP sibling instead.
- You're not building a Next.js / static-output site.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `artifact-bucket` | yes | - | Target S3/GCS bucket for the tarball |
| `artifact-prefix` | no | `artifacts` | Bucket prefix for artifact uploads |
| `pipeline-name` | no | `build` | Verb users type after `sparkwing run` |
