# test-shards

Run a test suite split into N parallel shards, gated by a node that
passes only when every shard passes. Fully local: it runs end-to-end
with `sparkwing run`, no cloud or cluster.

## Scaffold

```sh
sparkwing pipeline new --name test-shards --template test-shards \
  --param shards=4 --param test-cmd="go test ./..."
```

## What it does

- A Plan-layer `JobFanOut` registers one shard Job per index
  (`shard-0` through `shard-{N-1}`), all dependency-free so they
  dispatch in parallel.
- Each shard runs `test-cmd` with `SHARD_INDEX` (0-based) and
  `SHARD_TOTAL` exported, so the command can select its slice: via a
  test runner's native `--shard` flag, or `go test -run` over a subset.
- A `gate` Job `Needs` the whole shard group, so it runs (and the run
  succeeds) only when every shard passed. If any shard fails, the gate
  is cancelled and the run fails.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `test-shards` | pipeline registration name |
| `shards` | no | `4` | number of parallel shards |
| `test-cmd` | no | `go test ./...` | command per shard; reads `SHARD_INDEX`/`SHARD_TOTAL` |

## Notes

- Paths/commands resolve against the repo root (`WorkDir()`), not
  `.sparkwing/`.
- The default `test-cmd` ignores the shard env (runs the whole suite in
  every shard); wire `SHARD_INDEX`/`SHARD_TOTAL` into your runner to
  actually split work.
