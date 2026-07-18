# lint-test-go

The smallest useful template. A three-node DAG that runs gofmt, go
vet, and go test in parallel, with no cloud / registry / gitops
concerns.

Good as a first read for anyone learning the SDK shape: the Plan
signature is the canonical one, each step is independent so the
parallel-DAG mechanics are visible, and it shows both command
primitives side by side. gofmt and test go through `sparkwing.Bash`
because they need a shell -- gofmt for its `$(gofmt -l .)` guard, test
so a free-form `test-args` value word-splits. vet is fixed argv, so it
uses `sparkwing.Exec` (no shell, no quoting). Reach for Exec when you
can, Bash when you must.

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
| `pipeline-name` | no | `lint-test` | Verb users type after `sparkwing run` |
| `test-args` | no | `./...` | Extra args appended to `go test` (e.g. `-race ./...`) |

`vet` and `gofmt` always cover the whole module (`./...` and `.`); only
`test` is scoped via `test-args`.

## Scaffold

```sh
sparkwing pipeline new --name lint-test --template lint-test-go
```

## After rendering

Add a wider Go-lint set by copying one of the check methods and wiring
another `sparkwing.Job(...)` in `Plan`:

- staticcheck: `sparkwing.Exec(ctx, "staticcheck", "./...")`.
- golangci-lint: `sparkwing.Exec(ctx, "golangci-lint", "run")`.

Each is an independent node, so it reports alongside the others in a
single run.
