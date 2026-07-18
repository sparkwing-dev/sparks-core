# cached-test-suite

Run a test suite as a single content-cached Job. The cache key is the
combined content hash of the tracked files matching `source-globs`, so
an unchanged tree replays the recorded pass instead of re-running the
suite. Fully local: the replay is observable with two `sparkwing run`
invocations.

## Scaffold

```sh
sparkwing pipeline new --name test --template cached-test-suite \
  --param test-cmd="go test ./..." \
  --param source-globs="*.go,go.mod,go.sum"
```

## What it does

- The Plan registers one `test` Job and attaches `.Cache(...)`. The key
  comes from `contentkey.Salted(cache-version, globs...)`, which runs
  `git ls-files` then `git hash-object` over the matched paths and folds
  the sorted `(path, blob-hash)` pairs into a `sparkwing.CacheKey`.
- First run on a given tree: the key misses, the job runs `test-cmd`,
  and the pass is stored under that key with the `cache-ttl` retention.
- Later run on identical content: the key hits, so the orchestrator
  replays the stored pass and never executes `test-cmd`.
- Change any matched file (or bump `cache-version`) and the key busts,
  so the suite runs again and stores a fresh result.

Because the key is content-addressed, it hits across branches, rebases,
and machines whenever the matched file set has the same content. It is
not tied to a commit or a base ref.

## When to use

- You want an unchanged commit to skip re-executing tests by replaying
  a stored pass, keyed on file content.
- You rebase or switch branches often and want a matching tree to reuse
  an earlier pass rather than re-run.

## When NOT to use

- You want the suite to always run (a plain gate): use `lint-test-go`,
  or `test-shards` to split a slow suite across parallel shards.
- You want to skip a job entirely when its inputs did not change versus
  a base branch, storing nothing: use `skip-if-paths-unchanged`, which
  decides with `git diff` against a base ref instead of replaying a
  stored result.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `test` | Verb users type after `sparkwing run` |
| `test-cmd` | no | `go test ./...` | Command run when the key misses (any stack) |
| `source-globs` | no | `*.go,go.mod,go.sum` | Comma list of git pathspecs hashed into the key (`*.go` matches recursively) |
| `cache-ttl` | no | `168h` | Go duration a stored pass stays reusable (clamped to 840h; empty uses the SDK default) |
| `cache-version` | no | `v1` | Salt folded into the key; bump to invalidate every stored result |

## After rendering

- Content hashing sees file bytes, not your toolchain. A Go, Node, or
  compiler upgrade that changes test outcomes without touching a matched
  file will not bust the key. Bump `cache-version` after such an upgrade
  to invalidate every stored pass at once.
- Point `source-globs` at everything a test outcome depends on. If a
  fixture or config file can change results, add it to the globs, or a
  stale pass may replay.
- The default globs are Go pathspecs. Swap them for your stack
  (`src/**,package.json,package-lock.json`, `*.py,pyproject.toml`,
  ...) and set `test-cmd` to match. A plain `*.ext` git pathspec
  matches that extension at any depth; a `**/` prefix skips root-level
  files, so avoid it unless you mean to.
