# sparks-core/templates

Curated pipeline template registry. Each template is a deterministic,
parameterized starting point for a sparkwing pipeline -- the simplified
canonical version of patterns that already work in production consumer
repos.

Pull a template via the sparkwing CLI:

```sh
sparkwing pipeline templates                               # list available
sparkwing pipeline templates --name static-deploy-s3-cloudfront
sparkwing pipeline new --name deploy \
    --template static-deploy-s3-cloudfront \
    --param bucket=mysite \
    --param distribution=ABCD1234
```

Or read them directly here: each template is a directory containing a
`template.yaml` manifest, a `pipeline.go.tmpl` Go-template body, and a
`README.md` explaining when to use it.

## Templates

38 templates. The verify column is the tier the registry harness
holds each template to on every sparkwing release: runnable templates run
green locally, dry-runnable templates run green with SPARKWING_DRY_RUN=1
and touch no infrastructure, compile-only templates are rendered, compiled,
linted, and explained.

| Name | Cloud | Category | Verify | Notes |
|---|---|---|---|---|
| [static-deploy-s3-cloudfront](static-deploy-s3-cloudfront/) | aws | static-site-deploy | compile-only | Build a Next.js (or any static-output) app and sync to S3 + CloudFron... |
| [static-deploy-gcs-cloudcdn](static-deploy-gcs-cloudcdn/) | gcp | static-site-deploy | compile-only | Build a Next.js (or any static-output) app and sync to a Google Cloud... |
| [docker-deploy-ecr-eks](docker-deploy-ecr-eks/) | aws | docker-deploy | compile-only | Build a Docker image, push to ECR, and deploy via gitops to EKS (or k... |
| [docker-deploy-gar-gke](docker-deploy-gar-gke/) | gcp | docker-deploy | compile-only | Build a Docker image, push to Google Artifact Registry, and deploy vi... |
| [go-test-build-deploy-k8s](go-test-build-deploy-k8s/) | aws | docker-deploy | compile-only | Test, build a Docker image to ECR, and deploy to Kubernetes by applyi... |
| [go-test-migrate-deploy-argo](go-test-migrate-deploy-argo/) | aws | docker-deploy | compile-only | Run integration tests against an ephemeral Postgres, build a Docker i... |
| [approval-gated-deploy](approval-gated-deploy/) | any | deploy | compile-only | Build -> test -> human approval gate -> deploy |
| [next-build-and-push](next-build-and-push/) | aws,gcp | build-only | compile-only | Build a Next.js static export and upload the artifact tarball (out.ta... |
| [build-publish-binary](build-publish-binary/) | any | build | runnable | Build a versioned Go binary, write a SHA-256 checksum, and publish bo... |
| [docker-build-smoketest](docker-build-smoketest/) | any | build | runnable | Build a Docker image from a Dockerfile and smoke-test that it runs |
| [lint-test-go](lint-test-go/) | any | ci-hygiene | runnable | CI hygiene gate for Go projects: gofmt, go vet, go test |
| [test-shards](test-shards/) | any | testing-strategies | runnable | Run a test suite split into N parallel shards, gated by a node that p... |
| [integration-test-with-service](integration-test-with-service/) | any | testing-strategies | runnable | Run integration tests against a throwaway service container (Postgres... |
| [scheduled-cleanup](scheduled-cleanup/) | any | maintenance | runnable | A scheduled maintenance pipeline: delete files under a directory olde... |
| [container-deploy-ecs-fargate](container-deploy-ecs-fargate/) | aws | container-service-deploy | dry-runnable | Test, build a Docker image, push it to ECR, and roll it out to an ECS... |
| [docker-deploy-gar-cloudrun](docker-deploy-gar-cloudrun/) | gcp | container-service-deploy | dry-runnable | Test, build a Docker image, push it to Google Artifact Registry, and ... |
| [cloudrun-deploy-source](cloudrun-deploy-source/) | gcp | container-service-deploy | dry-runnable | Deploy a service to Cloud Run directly from source with `gcloud run d... |
| [gke-deploy-gar-kubectl](gke-deploy-gar-kubectl/) | gcp | docker-deploy | compile-only | Test, build a Docker image to Google Artifact Registry, fetch GKE clu... |
| [lambda-deploy](lambda-deploy/) | aws | serverless-function-deploy | dry-runnable | Deploy an AWS Lambda from either a container image (build + push to E... |
| [cloud-functions-deploy](cloud-functions-deploy/) | gcp | serverless-deploy | dry-runnable | Deploy a 2nd-gen Google Cloud Function from source with `gcloud funct... |
| [next-preview-deploy-cloudrun](next-preview-deploy-cloudrun/) | gcp | static-frontend | compile-only | Per-branch preview deploys for a server-rendered Next.js app on Cloud... |
| [canary-deploy-k8s](canary-deploy-k8s/) | aws,gcp | deploy | compile-only | Progressive canary rollout on Kubernetes: build the image, deploy a s... |
| [github-release-go](github-release-go/) | any | release-publish | dry-runnable | Cross-compile a Go binary for a GOOS/GOARCH matrix, write a SHA-256 c... |
| [npm-publish-package](npm-publish-package/) | any | release-publish | dry-runnable | Build an npm package and publish it with `npm publish`, gated so the ... |
| [pypi-publish-wheel](pypi-publish-wheel/) | any | release-publish | dry-runnable | Build a Python sdist and wheel with `python -m build`, validate them ... |
| [container-publish-multiarch](container-publish-multiarch/) | aws,gcp | release-publish | compile-only | Build a multi-arch (amd64 + arm64) container image with `docker build... |
| [lint-test-node](lint-test-node/) | any | ci-hygiene | runnable | CI hygiene gate for a Node/TypeScript project: install, then lint (es... |
| [lint-test-python](lint-test-python/) | any | ci-hygiene | runnable | CI hygiene gate for a Python project on the uv/ruff/pytest stack: uv ... |
| [test-matrix](test-matrix/) | any | testing-strategies | runnable | Fan the full test suite out across a matrix of toolchain versions and... |
| [coverage-gated-test](coverage-gated-test/) | any | testing-strategies | runnable | Run a test suite that emits a coverage report, then a Verify postcond... |
| [cached-test-suite](cached-test-suite/) | any | caching-skip | runnable | Run a test suite as a single .Cache()-keyed Job whose content key is ... |
| [skip-if-paths-unchanged](skip-if-paths-unchanged/) | any | caching-skip | runnable | A CI job guarded by .SkipIf(contentkey.Unchanged(base, paths...)): it... |
| [docker-build-layer-cache](docker-build-layer-cache/) | aws,gcp | caching-skip | runnable | Build a Docker image with BuildKit layer caching wired for reuse acro... |
| [terraform-plan-pr](terraform-plan-pr/) | aws,gcp | terraform | dry-runnable | Run terraform init + terraform plan against a Terraform root and surf... |
| [terraform-apply-gated](terraform-apply-gated/) | aws,gcp | terraform | compile-only | terraform plan -> human approval gate -> terraform apply |
| [db-migrate-updown](db-migrate-updown/) | any | database | runnable | Run golang-migrate as its own pipeline, decoupled from any build or d... |
| [db-backup-restore-drill](db-backup-restore-drill/) | aws,gcp | database | runnable | Disaster-recovery drill: dump the source database, restore that dump ... |
| [scheduled-db-backup](scheduled-db-backup/) | aws,gcp | maintenance | compile-only | A cron-scheduled database backup: dump the database behind a sparkwin... |

## Programmatic access

The Go module exports an `embed.FS` over the entire registry plus
parsed manifests. See `templates.go`. Agents consuming the registry
should read manifests via `Manifest()` / `List()` rather than parsing
YAML directly.

## Authoring a new template

1. Create a new directory: `templates/<name>/`.
2. Add `template.yaml` (see existing entries for shape).
3. Add `pipeline.go.tmpl` -- Go's `text/template` syntax. Parameters
   declared in `template.yaml.parameters` are accessible as
   `{{.<name>}}`.
4. Add `README.md` -- one paragraph describing what it does and when
   to use it.
5. Add the entry to the `templateNames` list in `templates.go`.
6. Bump the changelog. Tag `templates/v<X.Y.Z>`.

The template body should be **the simplified canonical version of
what already works in production**. Pull from real consumer repos
(consumer pipelines) rather than inventing shapes.
