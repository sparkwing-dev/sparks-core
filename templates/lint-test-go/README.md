# lint-test-go

The smallest useful template. A three-node DAG that runs gofmt, go
vet, and go test in parallel, with no cloud / registry / gitops
concerns.

Good as a first read for anyone learning the SDK shape: every node is
a `sparkwing.Bash` call, the Plan signature is the canonical one, and
each step is independent so the parallel-DAG mechanics are visible.

## When to use

- You want a CI hygiene gate that runs on every push or pre-commit.
- You need a starter pipeline to learn the SDK with -- this is the
  least-distracting option.
- You're scaffolding a fresh Go service and just need something
  compilable to iterate from.

## When NOT to use

- You're not on Go.
- You actually want to deploy something -- pick a build/deploy
  template instead.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `go-version` | no | `1.26` | Banner version (real version is pinned in go.mod) |
| `pipeline-name` | no | `lint-test` | Verb users type after `wing` |
| `test-args` | no | `./...` | Extra args appended to `go test` |

## After rendering

Add staticcheck or golangci-lint as additional nodes if your project
uses them; the pre-commit pipelines in sparkwing-platform / rangz-web
have examples of the wider Go-lint set.
