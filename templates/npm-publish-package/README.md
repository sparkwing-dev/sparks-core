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
   matching git tag (`v<version>`) already exists, so a forgotten version
   bump refuses to re-publish an already-released version.
2. `build` (only when `build-cmd` is non-empty) runs the build command in
   `package-dir`.
3. `publish` runs `npm publish` with `--registry`, `--access`, and,
   when `provenance` is set, `--provenance`. The token named by
   `token-secret` is exported as `NODE_AUTH_TOKEN`.

The publish step honors the dry-run convention from the release block:
while `dry-run` is set (the default) or `SPARKWING_DRY_RUN` is in the
environment, it logs the exact `npm publish` argv it would run and reaches
no registry. Clear `dry-run` and provide the token to publish for real.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-npm` | pipeline registration name |
| `package-dir` | no | `.` | directory holding `package.json`, relative to repo root |
| `build-cmd` | no | `npm run build` | build command run before publish; empty disables the step |
| `registry` | no | `https://registry.npmjs.org` | target npm registry URL |
| `access` | no | `public` | `npm publish --access` value: `public` or `restricted` |
| `provenance` | no | `true` | publish with `--provenance` when non-empty |
| `token-secret` | no | `NPM_TOKEN` | sparkwing secret holding the npm auth token |
| `dry-run` | no | `true` | echo the publish command instead of running it when non-empty |

## Going live

1. Bump the `version` in `package.json` (an already-tagged version fails
   the guard).
2. Store the npm auth token as the sparkwing secret named by
   `token-secret` (default `NPM_TOKEN`).
3. Run with `--param dry-run=` to clear the dry run and publish.

## Notes

- Requires `node`/`npm` on `PATH` for the build step and a real publish.
  A dry run only needs git and the `package.json` file, so it runs with no
  toolchain.
- `--provenance` requires npm 9.5+ and a supported CI environment; clear
  `provenance` if your setup does not provide the OIDC attestation.
- For a Python package use `pypi-publish-wheel`; for downloadable binaries
  use `github-release-go`.
