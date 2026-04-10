# sparks-core/templates

Curated pipeline template registry. Each template is a deterministic,
parameterized starting point for a sparkwing pipeline -- the simplified
canonical version of patterns that already work in production consumer
repos.

Pull a template via the sparkwing CLI:

```sh
sparkwing pipeline templates                               # list available
sparkwing pipeline templates show static-deploy-s3-cloudfront
sparkwing pipeline new --name deploy \
    --template static-deploy-s3-cloudfront \
    --param bucket=mysite \
    --param distribution=ABCD1234
```

Or read them directly here: each template is a directory containing a
`template.yaml` manifest, a `pipeline.go.tmpl` Go-template body, and a
`README.md` explaining when to use it.

## Templates

| Name | Cloud | Category | Notes |
|---|---|---|---|
| [static-deploy-s3-cloudfront](static-deploy-s3-cloudfront/) | aws | static-site-deploy | Next.js / static export to S3 + CloudFront |
| [static-deploy-gcs-cloudcdn](static-deploy-gcs-cloudcdn/) | gcp | static-site-deploy | Static export to GCS + Cloud CDN |
| [docker-deploy-ecr-eks](docker-deploy-ecr-eks/) | aws | docker-deploy | Build, push to ECR, gitops deploy to EKS |
| [docker-deploy-gar-gke](docker-deploy-gar-gke/) | gcp | docker-deploy | Build, push to GAR, gitops deploy to GKE |
| [next-build-and-push](next-build-and-push/) | any | build-only | Build a Next.js artifact, no deploy |
| [lint-test-go](lint-test-go/) | any | ci-hygiene | gofmt + go vet + go test |

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
(rangz-web, moonborn-ws, etc.) rather than inventing shapes.
