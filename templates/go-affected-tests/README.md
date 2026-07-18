# go-affected-tests

Run each Go package as its own content-cached test job, gated by a node
that passes only when every package job passes. Each key is the content
hash of that package's own files plus its same-module dependency closure
(resolved with `go list`), so an unchanged package replays its recorded
pass while a change re-runs only that package and the packages that
import it. Fully local: the per-package replay is observable with two
`sparkwing run` invocations.

## Scaffold

```sh
sparkwing pipeline new --name test --template go-affected-tests \
  --param packages="./api,./worker,./internal/store" \
  --param extra-key-globs="go.mod,go.sum"
```

## What it does

- The Plan splits the `packages` parameter into `go list` package specs
  and registers one `test-<name>` Job per spec, each with a `.Cache(...)`
  whose key comes from `contentkey.SaltedGoPackage(salt, spec, globs...)`.
  A `gate` Job `Needs` the whole group, so the run is green only when
  every package passed.
- `contentkey.SaltedGoPackage` resolves the package's same-module
  dependency closure with `go list -deps -test`, then hashes the tracked
  content of that closure (plus `extra-key-globs`) into a
  `sparkwing.CacheKey`. The salt combines the pipeline name,
  `cache-version`, and `test-cmd`, and the spec itself, so no two package
  jobs ever replay one another's pass.
- First run on a package's content: the key misses, the job runs
  `test-cmd` with `PACKAGE` set to the spec, and the pass is stored.
- Later run with that package's closure unchanged: the key hits, so the
  orchestrator replays the stored pass and never re-runs the package.
- Edit a package, or a same-module package it imports, and only that
  package's key (and its dependents') busts, so only the affected jobs
  re-run while the rest still hit.

Because each key is content-addressed, it hits across branches, rebases,
and machines whenever a package's closure has the same content.

## When to use

- A Go monorepo with many packages where editing one package should
  re-run only that package's tests, not the whole suite.
- You want a shared-library edit to correctly bust its dependents: the
  dependency closure is part of each key, so a change to an imported
  package re-runs the packages that import it.

## When NOT to use

- One suite, one key, no per-package granularity: use `cached-test-suite`,
  which caches the whole suite under a single content key that busts on
  any matched change.
- You want to skip a job outright (storing nothing) when its inputs did
  not change versus a base branch: use `skip-if-paths-unchanged`, which
  decides with `git diff` against a base ref.
- You want to speed a single suite across parallel runners rather than
  cache per package: use `test-shards`.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `test` | Verb users type after `sparkwing run` |
| `packages` | no | `.,./integration` | Comma list of `go list` package specs, one cached test job each |
| `test-cmd` | no | `go test $PACKAGE` | Command run per package when its key misses (`PACKAGE` exported) |
| `extra-key-globs` | no | `go.mod,go.sum` | Extra tracked git pathspecs folded into every package's key |
| `cache-version` | no | `v1` | Salt folded into every key; bump to invalidate all stored results |

## After rendering

- List the packages you want cached independently in `packages`. Plan is
  pure-declarative (no runtime `go list` while the DAG is built), so the
  set of jobs is fixed by this static list; add or remove entries as your
  package layout changes.
- Each key already covers a package's same-module dependency closure, so
  you do not list a package's dependencies -- editing an imported package
  busts the importer's key automatically. Put in `extra-key-globs` only
  the repo-wide inputs a package hash cannot see (`go.mod`, `go.sum`,
  shared testdata a package reads at test time).
- Content hashing sees file bytes, not your toolchain. A Go upgrade that
  changes test outcomes without touching a tracked file will not bust any
  key; bump `cache-version` after such an upgrade to invalidate every
  stored pass at once.
- The cache key is computed with `go list`, so it resolves against the
  module at the repo root. In a repo whose checkouts sit under a parent
  `go.work`, run with `GOWORK=off` (or without the workspace) so `go list`
  resolves the local module rather than the workspace.
