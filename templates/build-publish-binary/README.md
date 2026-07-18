# build-publish-binary

Build a versioned Go binary, write a SHA-256 checksum, and publish both
into a local release directory. Fully local, with no cloud, registry,
or cluster, so it runs end-to-end with `sparkwing run` on a laptop.

## Scaffold

```sh
sparkwing pipeline new --name release-binary --template build-publish-binary \
  --param main-package=./cmd/app --param binary-name=app --param release-dir=dist
```

## What it does

One `build-publish` job:

1. Derives a version from `git describe --tags --always --dirty`
   (falls back to `dev` in a repo with no tags/commits).
2. Compiles `main-package` with the version stamped via
   `-ldflags "-X main.Version=<version>"`.
3. Writes the binary and a `<binary>.sha256` checksum into `release-dir`.

A `.Verify` postcondition re-hashes every published binary against its
recorded checksum, failing the run at the verify stage on a mismatch.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `release-binary` | pipeline registration name |
| `main-package` | no | `./cmd/app` | `go build` target, relative to repo root |
| `binary-name` | no | `app` | output binary file name |
| `release-dir` | no | `dist` | publish directory, relative to repo root |

## Notes

- Paths resolve against the repo root (`WorkDir()`), not `.sparkwing/`.
- For container images use a docker-deploy template; for static sites
  use a static-deploy template.
