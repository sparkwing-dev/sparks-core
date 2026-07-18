# test-matrix

Fan the full test suite out across a matrix of toolchain versions and
OS runner labels, one parallel job per combination, gated by a node that
passes only when every combination passes. The default single-dimension
matrix runs end-to-end with `sparkwing run`, no cloud or cluster.

Use this to prove one suite passes across every `{version} x {OS}`
combination (compatibility / regression testing). It runs the WHOLE
suite once per cell, so it trades runtime for coverage. To go faster by
splitting one suite across shards, use test-shards instead. For a single
unsharded pass, use lint-test-go. For a suite that needs a live
dependency (Postgres, Redis), use integration-test-with-service.

## Scaffold

```sh
sparkwing pipeline new --name test-matrix --template test-matrix \
  --param versions=1.24,1.25,1.26 --param test-cmd="go test ./..."
```

Add a second dimension of OS runner labels:

```sh
sparkwing pipeline new --name test-matrix --template test-matrix \
  --param versions=1.24,1.25 --param os-labels=linux,darwin
```

Isolate each version in its own container image (no local version
manager):

```sh
sparkwing pipeline new --name test-matrix --template test-matrix \
  --param versions=20,22 --param container-image-repo=node \
  --param test-cmd="npm ci && npm test"
```

## What it does

- `Plan` builds the Cartesian product of `versions` and `os-labels` from
  their comma-lists, then a `JobFanOut` registers one cell Job per
  combination, all dependency-free so they dispatch in parallel.
- Each cell exports `MATRIX_VERSION` into the test command's
  environment. The command itself decides what to do with it (select a
  toolchain via `setup-cmd`, tag its report, or pin an image).
- The OS label, when present, is applied as a soft `Prefers` (not a hard
  `Requires`), so a single-runner laptop still dispatches every cell
  while a labelled runner pool routes each cell to its OS.
- A `gate` Job `Needs` the whole matrix group, so it runs (and the run
  succeeds) only when every cell passed. If any cell fails, the gate is
  cancelled and the run fails.

### Container-per-version mode

Set `container-image-repo` to run each version cell inside a throwaway
Docker container instead of on the host: the cell runs
`docker run --rm <repo>:<version>` with the repo bind-mounted at `/src`
and `MATRIX_VERSION` exported, then executes `setup-cmd && test-cmd`
inside it. Because setup and test share the one in-container shell, a
`setup-cmd` here prepares the container (not the host). This needs a
Docker daemon and pulls `<repo>:<version>` for each version, so it does
not run without local Docker; the host mode (empty
`container-image-repo`) is the fully local default.

The in-container command runs under `bash -lc`, so the image tag must
ship bash. The official Debian-based tags (`node:20`, `python:3.11`,
`golang:1.24`) do; the `-alpine` variants ship only `sh`, so pick a
Debian-based tag or install bash in a derived image.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `test-matrix` | pipeline registration name |
| `versions` | no | `1.24,1.25,1.26` | comma list of toolchain versions, one row each; exported as `MATRIX_VERSION` |
| `os-labels` | no | (empty) | comma list of OS runner labels forming the second dimension; soft `Prefers` |
| `container-image-repo` | no | (empty) | run each version in `docker run <repo>:<version>`; empty runs on the host |
| `setup-cmd` | no | (empty) | optional per-cell setup, run in the same shell as `test-cmd`, sees `MATRIX_VERSION` |
| `test-cmd` | no | `go test ./...` | test command per cell, `MATRIX_VERSION` exported |
| `timeout` | no | (empty) | optional per-cell timeout as a Go duration (e.g. `30m`); empty disables the cap |

## Notes

- Paths and commands resolve against the repo root (`WorkDir()`), not
  `.sparkwing/`.
- `MATRIX_VERSION` is a plain environment export. The default `test-cmd`
  ignores it (runs the whole suite unchanged in every cell); wire it into
  `setup-cmd` or into `container-image-repo` to actually vary the
  toolchain per cell.
- `setup-cmd` and `test-cmd` run in ONE shell (`setup-cmd && test-cmd`),
  so PATH and exported variables set by setup persist into the test. The
  shell is non-login, so a shim-based selector like asdf (which resolves
  versions via `PATH` shims) works, but `gvm use` / `nvm use` do not:
  they are shell functions that need a sourced login shell. Prefer asdf,
  or select the toolchain via `container-image-repo`.
- The OS dimension biases runner selection but never blocks a cell. To
  hard-pin a cell to an OS, change its `Prefers` to `Requires` in the
  rendered cell type after scaffolding.
