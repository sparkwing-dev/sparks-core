# pypi-publish-wheel

Build a Python sdist and wheel, validate them with `twine check`, and
upload to PyPI (or TestPyPI) with `twine upload`. The run is gated on the
project version being unreleased, so an already-published version is
never re-uploaded. Build and check run locally; the upload routes through
a block that honors `SPARKWING_DRY_RUN`.

## Scaffold

```sh
sparkwing pipeline new --name publish-pypi --template pypi-publish-wheel \
  --param repository=testpypi --param token-secret=PYPI_API_TOKEN
```

The scaffold always emits the upload job. By default (`dry-run=true`) that
job echoes its `twine upload` argv instead of reaching the index, so the
first `sparkwing run` builds and validates without publishing. To publish
for real, scaffold with `--param dry-run=` (empty) or flip `DryRun` to
`false` in the generated upload call.

## What it does

A linear DAG of jobs:

1. `guard-version` reads the version declared in `version-file` (resolved
   under `package-dir`, `pyproject.toml` by default) and fails the run if
   a git tag for that version already exists. Set `version-field` to read
   a non-default key (for example `tool.poetry.version` for Poetry). Bump
   the version to publish.
2. `build` runs `build-cmd` (`python -m build` by default) in
   `package-dir`, producing the sdist and wheel under `dist/`.
3. `twine-check` runs `twine check dist/*` to validate the built
   distributions.
4. `upload` runs `twine upload` to `repository`, authenticating with the
   token from `token-secret` exported as `TWINE_PASSWORD` (username
   `__token__`). While `dry-run` is set it echoes its argv instead of
   reaching the index.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-pypi` | pipeline registration name |
| `package-dir` | no | `.` | directory with `pyproject.toml`, relative to repo root |
| `build-cmd` | no | `python -m build` | command that builds the sdist and wheel into `dist/` |
| `repository` | no | `testpypi` | `twine` repository target: `testpypi` or `pypi` |
| `version-file` | no | `pyproject.toml` | manifest whose declared version gates the release, relative to `package-dir` |
| `version-field` | no | `` | dotted version key; empty uses the file default (`project.version`) |
| `token-secret` | no | `PYPI_API_TOKEN` | sparkwing secret holding the index API token |
| `dry-run` | no | `true` | when non-empty, the upload echoes its argv instead of publishing |

## When to use

- Pick over `github-release-go` when consumers install with `pip install
  your-pkg`, not by downloading a binary.
- The Node sibling is `npm-publish-package`.
- Set `repository=testpypi` to rehearse against TestPyPI first, then flip
  to `pypi` for the real upload.

## Notes

- The upload honors `SPARKWING_DRY_RUN`: when that variable is set the
  step echoes the exact `twine upload` argv and returns success without
  reaching the index, so the full path is safe to exercise before a
  token is in place. The `dry-run` param bakes the same behavior into the
  scaffold so it is green by default.
- TestPyPI and PyPI use separate credentials: a TestPyPI upload needs a
  TestPyPI token, not a PyPI one, even though the default secret name is
  `PYPI_API_TOKEN`. Point `token-secret` at the secret holding the token
  for whichever index `repository` targets.
- The version gate reads repository state only (the version file and the
  local tag list) and always runs for real, including under a dry run.
- Requires the build command's toolchain (`python` and the `build` module
  by default) and `twine` on `PATH`. For a Poetry project that declares
  its version under `[tool.poetry]`, set `version-field=tool.poetry.version`.
