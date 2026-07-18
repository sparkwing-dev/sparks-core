# npm-publish-package

Build an npm package and publish it with `npm publish`, gated so the
`package.json` version must not already be tagged. The publish step
defaults to a dry run that echoes its command, so the scaffold is green
before you wire a token.

## Scaffold

```sh
sparkwing pipeline new --name publish-npm --template npm-publish-package \
  --param package-dir=. --param build-cmd="npm run build"
```

## What it does

A short DAG:

1. `guard-version` reads the version from `package.json` and fails if the
   matching git tag (`<tag-prefix><version>`, e.g. `v1.2.3`) already
   exists, so a forgotten version bump refuses to re-publish an
   already-released version. The check only fires when that tag actually
   exists in the checkout (see the guard note below).
2. `build` (only when `build-cmd` is non-empty) runs `install-cmd`
   (`npm ci` by default) and then the build command in `package-dir`.
3. `publish` runs `npm publish` with `--registry`, `--access`, the
   `--tag` from `dist-tag` when set, and, when `provenance` is set,
   `--provenance`. The token named by `token-secret` is exported as
   `NODE_AUTH_TOKEN`.

The publish step honors the dry-run convention from the release block:
while `dry-run` is set (the default) or `SPARKWING_DRY_RUN` is in the
environment, it logs the exact `npm publish` argv it would run and reaches
no registry. Clear `dry-run` and provide the token to publish for real.

The install and build run locally in both dry-run and real mode; only the
registry publish is skipped under dry-run. Clear `build-cmd` for a dry run
that needs no Node toolchain at all.

### The version guard

The guard only refuses a publish when the git tag `<tag-prefix><version>`
already exists in the checkout. This pipeline does not create that tag, so
the guard protects you only if each release is tagged separately (for
example `npm version <bump>`, which writes `package.json` and tags the
commit). Without a tag the guard passes and npm's own duplicate-version
`403` is the only backstop. In a monorepo where every package shares the
default `v<version>` tag namespace, give each instance its own
`tag-prefix` (such as `mypkg@`) so the guards don't collide.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-npm` | pipeline registration name |
| `package-dir` | no | `.` | directory holding `package.json`, relative to repo root |
| `install-cmd` | no | `npm ci` | dependency install run in `package-dir` before the build; empty disables the step |
| `build-cmd` | no | `npm run build` | build command run before publish; empty disables the build node |
| `registry` | no | `https://registry.npmjs.org` | target npm registry URL |
| `access` | no | `public` | `npm publish --access` value: `public` or `restricted` |
| `dist-tag` | no | `` | npm dist-tag (`--tag`); empty uses npm's default, `latest` |
| `provenance` | no | `` | publish with `--provenance` when non-empty (needs an OIDC CI) |
| `tag-prefix` | no | `v` | prefix joined to the version to form the guarded git tag |
| `token-secret` | no | `NPM_TOKEN` | sparkwing secret holding the npm auth token |
| `dry-run` | no | `true` | echo the publish command instead of running it when non-empty |

## Going live

1. Bump the `version` in `package.json` and tag the release
   `<tag-prefix><version>` so the guard can protect it. With the default
   `v` prefix, `npm version <patch|minor|major>` does both at once (it
   writes `package.json` and tags the commit `v<version>`); a custom
   `tag-prefix` needs the matching tag created by hand. An already-tagged
   version fails the guard.
2. Store the npm auth token as the sparkwing secret named by
   `token-secret` (default `NPM_TOKEN`).
3. Run with `--param dry-run=` to clear the dry run and publish. Add
   `--param dist-tag=next` (or similar) to publish a prerelease off the
   `latest` tag.

## Notes

- Requires `node`/`npm` on `PATH` whenever `build-cmd` is set (the install
  and build run locally even under dry-run) and for a real publish. Clear
  `build-cmd` for a dry run that needs only git and the `package.json`
  file.
- `install-cmd` runs `npm ci` by default so the build has its
  dependencies on a clean runner. Clear it if dependencies are already
  installed, or point it at `npm install` / `pnpm install` / `yarn` to
  match your project.
- `provenance` is off by default because `--provenance` requires npm 9.5+
  running inside an npm-supported OIDC CI (such as GitHub Actions or
  GitLab CI); a publish from a generic runner without that attestation
  fails. Set `--param provenance=true` only on a supported CI.
- For a Python package use `pypi-publish-wheel`; for downloadable binaries
  use `github-release-go`.
