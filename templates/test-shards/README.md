# test-shards

Run a test suite split into N parallel shards, gated by a node that
passes only when every shard passes. The default `test-cmd` splits the
module's packages across the shards, so an unedited scaffold divides the
work rather than repeating it. It runs end-to-end with `sparkwing run`,
no cloud or cluster; the wall-clock speedup, though, only lands when the
shards dispatch to separate runners (see Notes).

## Scaffold

```sh
sparkwing pipeline new --name test-shards --template test-shards \
  --param shards=4
```

Target a labelled runner fleet so the shards spread across it:

```sh
sparkwing pipeline new --name test-shards --template test-shards \
  --param shards=4 --param runner-label=ci-linux
```

## What it does

- A Plan-layer `JobFanOut` registers one shard Job per index
  (`shard-0` through `shard-{N-1}`), all dependency-free so they
  dispatch in parallel. Plan rejects `shards` below 1 so a zero or
  negative value fails loudly instead of producing a gate over an empty
  group.
- Each shard runs `test-cmd` with `SHARD_INDEX` (0-based) and
  `SHARD_TOTAL` exported. The default splits `go list ./...` by list
  position (`awk` keeps every Nth package), so each shard tests a
  disjoint slice and the slices together cover the whole module.
- With `runner-label` set, every shard carries a soft `Prefers` for that
  label so a labelled pool routes the shards across its runners; it stays
  a preference, so a single-runner setup still dispatches every shard.
- A `gate` Job `Needs` the whole shard group, so it runs (and the run
  succeeds) only when every shard passed. If any shard fails, the gate
  is cancelled and the run fails.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `test-shards` | pipeline registration name |
| `shards` | no | `4` | number of parallel shards (must be >= 1) |
| `test-cmd` | no | `go test $(go list ./... \| awk ...)` | command per shard; reads `SHARD_INDEX`/`SHARD_TOTAL` |
| `runner-label` | no | (empty) | soft `Prefers` label every shard targets |

## Notes

- Paths/commands resolve against the repo root (`WorkDir()`), not
  `.sparkwing/`.
- The speedup is real only across multiple runners: on a single runner
  the shards serialize and share one CPU, so total runtime is unchanged.
  Point `runner-label` at a labelled fleet to spread them.
- The default splits by package. Setting `shards` higher than the number
  of packages leaves the surplus shards with nothing to run. To shard
  within a package instead, replace `test-cmd` with your runner's native
  `--shard` flag or a `go test -run` subset keyed off
  `SHARD_INDEX`/`SHARD_TOTAL`.
