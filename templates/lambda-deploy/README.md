# lambda-deploy

Deploy an AWS Lambda function and shift a named alias to the freshly
published version, from either of the two packaging modes:

- **image** -- build a container image from your Dockerfile, push it to
  ECR, and point the function at the new `--image-uri`.
- **zip** -- run your packager to produce a deployment archive, then
  update the function code (staged through S3 for large archives, or
  uploaded inline).

Either way the deploy publishes an immutable version and shifts an alias
(default `live`) to it. An optional post-deploy HTTP probe verifies the
new version and, on a definitive failure, shifts the alias back to the
version it pointed at before.

This is a **raw-composition** template: the generated `Plan()` wires
sparks-core blocks (`lambda`, `docker`, `probe`) into an optional-test
-> build -> deploy DAG directly, so you can see and edit the
orchestration. The blocks do the work; the scaffolded file is just the
shape.

The `package-type` param is resolved **at render time**, so the file you
get is only the branch you asked for: an image scaffold has no zip code
and vice versa. Re-scaffold (or edit the file) to switch modes.

## When to use

- You deploy an AWS Lambda, in either packaging mode.
- The workload is request- or event-driven and should scale to zero.
- Choose `package-type=image` when dependencies exceed the zip size
  limit or you need a custom runtime; choose `package-type=zip` (node,
  python, go, rust) when the bundle fits and you want faster cold
  starts.

## When not to use

- You run a long-lived container that should not scale to zero: use
  `container-deploy-ecs-fargate`.
- You are on GCP: use the source-function twin `cloud-functions-deploy`.
- You want a container image published to a registry but not rolled out
  to any function: use `container-publish-multiarch`.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `function-name` | yes | | Lambda function name to update |
| `package-type` | no | `zip` | `image` (build + push ECR) or `zip` (build + package). Branched at render time |
| `image` | no | | Image name (`package-type=image`) |
| `ecr` | no | | ECR registry URL Lambda pulls from (`package-type=image`) |
| `build-cmd` | no | | Command producing the zip at `zip-path` (`package-type=zip`); empty disables the build node |
| `zip-path` | no | `function.zip` | Path to the zip `build-cmd` produces (`package-type=zip`) |
| `artifact-bucket` | no | | S3 bucket to stage large zips through; empty updates code inline (`package-type=zip`) |
| `alias` | no | `live` | Alias shifted to the freshly published version |
| `health-url` | no | | Optional URL the post-deploy probe checks; empty skips verification and rollback |
| `region` | no | `us-west-2` | AWS region of the function |
| `aws-profile` | no | | AWS profile for local runs; empty resolves via `AWS_PROFILE` or IRSA |
| `pipeline-name` | no | `build-deploy` | Name users type after `sparkwing run` |
| `test-cmd` | no | | Optional pre-build test command; empty disables the test node |

## After rendering

- **The alias must already exist.** The deploy shifts an existing alias;
  it does not create one. Create `alias` (and the function) with your
  infrastructure-as-code before the first run.
- **The zip path assumes `build-cmd` produces the archive.** Point
  `build-cmd` at your bundler and `zip-path` at what it writes. Leave
  `build-cmd` empty when a prior step already produced the archive.
- **Large zips need `artifact-bucket`.** Above the direct-upload limit,
  set it and the archive stages through S3 automatically; the branch
  lives inside the `lambda` block, so the scaffold does not change.
- **The probe accepts any 2xx.** Tighten it with `.ExpectStatus(200)` or
  `.ExpectJSON("status", "ok")` if your function URL returns structured
  output. Omit `health-url` for a Lambda with no HTTP surface -- then
  there is no probe and no alias rollback.

## Verifying locally (dry run)

Every cloud mutation in this template routes through the `lambda` block,
which honors the dry-run contract: export `SPARKWING_DRY_RUN=1` and the
deploy echoes the exact `aws` argv it would run (update-function-code,
update-alias, and the `s3 cp` stage when a bucket is set) and returns
success without touching AWS. The build or package step still runs for
real. That is how the default zip scaffold runs green on a laptop with
no AWS credentials:

    SPARKWING_DRY_RUN=1 sparkwing run build-deploy

## Requirements

The runner needs the `aws` CLI on PATH. For `package-type=image` it also
needs `docker` and a Dockerfile at the repo root. Profile and IRSA
resolution come from the `aws` block: set `aws-profile` for a local
named profile, or leave it empty to resolve via `AWS_PROFILE` or an
in-cluster runner's IRSA role.
