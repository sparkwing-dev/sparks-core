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

- `changelog` (default): the section named by `version`, read from
  `changelog-path` (default `CHANGELOG.md`). With `version` empty the top
  released section is used, and its heading supplies the version.
- `git`: the notes are the commit subjects since the previous tag. The
  version comes from `version` when set (e.g. `v1.2.3`), otherwise from
  `git describe --tags --match "v*"`. Run this mode on a commit that is
  exactly tagged, or set `version` explicitly: on an untagged commit
  `git describe` returns a commit description like `v1.2.3-5-gabcdef`,
  which would be published as the release tag verbatim. Set
  `refuse-dirty=true` to refuse deriving a version from a working tree
  with uncommitted changes.

The compiled binaries are stamped with the resolved version via
`-ldflags -X <version-var>=<version>` (default symbol `main.Version`), so
`app --version` can report it. The linker ignores the flag when the
symbol does not exist.

### Publishing safely

The publish honors the `SPARKWING_DRY_RUN` convention. With `dry-run`
left at its default `true`, or with `SPARKWING_DRY_RUN` set in the
environment, the `github-release` node logs the exact `gh release create`
argv and returns success without reaching GitHub. Set `dry-run` to any
other value (for example `dry-run=false`) to publish for real, which
reads the GitHub token from the `token-secret` sparkwing secret. Set
`draft=true` to cut a draft release for review, or `prerelease=true` to
mark an RC or beta tag as a prerelease.

After a live (non-dry-run) publish, the pipeline posts an optional Slack
announce. Point `slack-webhook-secret` at a sparkwing secret holding a
Slack incoming-webhook URL to enable it; leaving it empty skips the
announce, so a missing webhook never fails a release.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `release-github` | pipeline registration name |
| `main-package` | no | `./cmd/app` | `go build` target, relative to repo root |
| `binary-name` | no | `app` | base output binary name |
| `version` | no | `""` | release version; empty derives it (git describe or top changelog section) |
| `version-var` | no | `main.Version` | linker symbol the version is stamped into via `-ldflags -X` |
| `platforms` | no | `linux/amd64,linux/arm64,darwin/arm64` | comma-separated GOOS/GOARCH pairs |
| `release-dir` | no | `dist` | output directory for artifacts and `checksums.txt` |
| `notes-source` | no | `changelog` | `changelog` or `git` |
| `changelog-path` | no | `CHANGELOG.md` | changelog file read in `changelog` mode |
| `refuse-dirty` | no | `false` | in `git` mode, refuse a dirty working tree |
| `token-secret` | no | `GITHUB_TOKEN` | sparkwing secret holding the GitHub token |
| `draft` | no | `false` | create the release as a draft |
| `prerelease` | no | `false` | mark the release as a prerelease |
| `slack-webhook-secret` | no | `""` | sparkwing secret holding a Slack webhook URL to announce a live release |
| `dry-run` | no | `true` | `true` echoes the publish; any other value publishes for real |

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
- The release `Tag` is the resolved version verbatim. Set `version` to
  pin an exact tag when your convention differs from your changelog
  headings or git tags.
- `gh` must be on PATH for a live publish; the `go` toolchain must be on
  PATH for the cross-build.
