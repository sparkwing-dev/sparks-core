# github-release-go

Cross-compile a Go binary across a GOOS/GOARCH matrix, write a SHA-256
checksums file, and cut a GitHub Release with the artifacts and release
notes via `gh release create`. The cross-build, checksum, and notes
derivation run locally; the publish step defaults to a dry-run that
echoes the `gh` invocation, so a freshly scaffolded pipeline runs green
with no token configured.

## Scaffold

```sh
sparkwing pipeline new --name release-github --template github-release-go \
  --param main-package=./cmd/app --param binary-name=app \
  --param platforms=linux/amd64,linux/arm64,darwin/arm64
```

## What it does

A linear three-node DAG:

1. `cross-build` resolves the release version and notes, then compiles
   every `GOOS/GOARCH` pair in `platforms` into `release-dir`, naming
   each artifact `<binary>_<version>_<goos>_<goarch>`.
2. `checksum` writes a `checksums.txt` sha256sum manifest over the built
   artifacts.
3. `github-release` cuts the GitHub Release with `gh release create`,
   uploading the artifacts and `checksums.txt` and attaching the notes.

### Version and notes

`notes-source` selects where the version and release notes come from,
branched at render time so the generated body stays static:

- `changelog` (default): the top released section of `CHANGELOG.md`
  supplies both the notes body and the version (the section heading).
- `git`: the version comes from `git describe --tags --match "v*"` and
  the notes are the commit subjects since the previous tag.

### Publishing safely

The publish honors the `SPARKWING_DRY_RUN` convention. With `dry-run`
non-empty (the default), or with `SPARKWING_DRY_RUN` set in the
environment, the `github-release` node logs the exact `gh release create`
argv and returns success without reaching GitHub. Set `dry-run=""` to
publish for real, which reads the GitHub token from the `token-secret`
sparkwing secret.

After a live (non-dry-run) publish, the pipeline posts an optional Slack
announce read from the `SLACK_WEBHOOK_URL` environment variable. An empty
value is a safe no-op, so the announce never fails a release.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `release-github` | pipeline registration name |
| `main-package` | no | `./cmd/app` | `go build` target, relative to repo root |
| `binary-name` | no | `app` | base output binary name |
| `platforms` | no | `linux/amd64,linux/arm64,darwin/arm64` | comma-separated GOOS/GOARCH pairs |
| `release-dir` | no | `dist` | output directory for artifacts and `checksums.txt` |
| `notes-source` | no | `changelog` | `changelog` or `git` |
| `token-secret` | no | `GITHUB_TOKEN` | sparkwing secret holding the GitHub token |
| `dry-run` | no | `true` | non-empty echoes the publish; empty publishes for real |

## When to use

Pick over `build-publish-binary` when you want an actual GitHub Release
(a tag, release notes, and uploaded cross-platform assets) rather than
just a local `dist/` directory. For a language-registry publish use
`npm-publish-package` (Node) or `pypi-publish-wheel` (Python). For a
container image use `container-publish-multiarch`.

## Notes

- Paths resolve against the repo root (`WorkDir()`), not `.sparkwing/`.
- Cross-compilation runs with `CGO_ENABLED=0`, so a pure-Go program
  builds every listed platform from any host without a cross toolchain.
- The release `Tag` is the derived version verbatim. Adjust the tag or
  add a `v` prefix in the generated `build` method if your convention
  differs from your changelog headings or git tags.
- `gh` must be on PATH for a live publish.
