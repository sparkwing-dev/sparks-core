# next-build-and-push

Build a Next.js static export and upload the resulting `out/` as a
tarball to an S3 bucket. **No deploy step.** Useful for split
build/promote workflows where the deploy is a manually-gated separate
pipeline.

The rendered pipeline:

1. Runs `npm ci && npm run build` (override with `build-cmd`).
2. Tarballs the output directory to `out.tar.gz`.
3. Uploads the tarball to `s3://<bucket>/<prefix>/<sha>.tar.gz`, keyed by
   the commit SHA so a downstream consumer can locate it and map it back
   to a commit.

## When to use

- Your deploy gate is approval-bound and the build should run
  unconditionally on merge.
- You want build artifacts retained for forensics or rollback,
  addressable by commit SHA.
- A downstream cross-repo trigger pulls the artifact and deploys.

## When NOT to use

- The deploy should be automatic -- use `static-deploy-s3-cloudfront`
  or its GCP sibling `static-deploy-gcs-cloudcdn` instead.
- You're not building a Next.js / static-output site.
- You need the artifact on GCS rather than S3 (this template pushes to
  S3 only).

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `artifact-bucket` | yes | - | Target S3 bucket for the tarball |
| `artifact-prefix` | no | `artifacts` | Bucket prefix; object keyed at `<prefix>/<sha>.tar.gz` |
| `build-cmd` | no | `npm ci && npm run build` | Install + build command producing the output dir |
| `out-dir` | no | `out` | Build output directory to tar |
| `pipeline-name` | no | `build` | Verb users type after `sparkwing run` |
