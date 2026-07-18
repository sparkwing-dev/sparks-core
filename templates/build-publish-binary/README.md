# build-publish-binary

Build a versioned, statically linked Go binary, write a SHA-256 checksums
manifest, and publish both into a local release directory. Fully local,
with no cloud, registry, or cluster, so it runs end-to-end with
`sparkwing run` on a laptop.

## Scaffold

```sh
sparkwing pipeline new --name release-binary --template build-publish-binary \
  --param main-package=./cmd/app --param binary-name=app --param release-dir=dist
```

## What it does

One `build-publish` job:

1. Derives a version from `git describe --tags --always`
   (falls back to `dev` in a repo with no tags/commits).
2. Cross-builds the host platform with the version stamped via
   `-ldflags "-X <version-var>=<version>"`, using `-trimpath` and
   `CGO_ENABLED=0` for a reproducible, statically linked binary.
3. Writes the binary as `<binary>_<version>_<goos>_<goarch>` and a
   `checksums.txt` sha256sum-format manifest into `release-dir`.

A `.Verify` postcondition re-reads `checksums.txt` and re-hashes every
listed artifact, failing the run at the verify stage on a mismatch.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `release-binary` | pipeline registration name |
| `main-package` | no | `./cmd/app` | `go build` target, relative to repo root |
| `binary-name` | no | `app` | base output binary name |
| `version-var` | no | `main.Version` | linker symbol the version is stamped into |
| `release-dir` | no | `dist` | publish directory, relative to repo root |

## Notes

- Paths resolve against the repo root (`WorkDir()`), not `.sparkwing/`.
- Point `version-var` at your own symbol (`main.version`, a `build`
  package path, and so on); the linker ignores it when the symbol does
  not exist.
- For a real GitHub Release with cross-platform assets use
  github-release-go. For container images use a docker-deploy template;
  for static sites use a static-deploy template.
