# pypi-publish-wheel

Build a Python sdist and wheel with `python -m build`, validate them with
`twine check`, and upload to PyPI (or TestPyPI) with `twine upload`. The
run is gated on the project version being unreleased, so an
already-published version is never re-uploaded. Build and check run
locally; the upload routes through a block that honors `SPARKWING_DRY_RUN`.

## Scaffold

```sh
sparkwing pipeline new --name publish-pypi --template pypi-publish-wheel \
  --param repository=testpypi --param token-secret=PYPI_API_TOKEN
```

By default (`dry-run=true`) the scaffold stops after `twine check` and
emits no upload step, so the first `sparkwing run` builds and validates
without publishing. Re-render with `--param dry-run=` (empty) to include
the upload step once you are ready to publish.

## What it does

A linear DAG of jobs:

1. `guard-version` reads the version declared in `version-file`
   (`pyproject.toml` by default, field `project.version`) and fails the
   run if a git tag for that version already exists. Bump the version to
   publish.
2. `build` runs `python -m build` in `package-dir`, producing the sdist
   and wheel under `dist/`.
3. `twine-check` runs `twine check dist/*` to validate the built
   distributions.
4. `upload` (present only when `dry-run` is empty) runs `twine upload` to
   `repository`, authenticating with the token from `token-secret`
   exported as `TWINE_PASSWORD` (username `__token__`).

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `publish-pypi` | pipeline registration name |
| `package-dir` | no | `.` | directory with `pyproject.toml`, relative to repo root |
| `repository` | no | `testpypi` | `twine` repository target: `testpypi` or `pypi` |
| `version-file` | no | `pyproject.toml` | file whose declared version gates the release |
| `token-secret` | no | `PYPI_API_TOKEN` | sparkwing secret holding the PyPI API token |
| `dry-run` | no | `true` | when non-empty, stop after `twine check` and emit no upload step |

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
  token is in place.
- The version gate reads repository state only (the version file and the
  local tag list) and always runs for real, including under a dry run.
- Requires `python`, the `build` module, and `twine` on `PATH`. For a
  Poetry project set `version-file` to the file declaring the version and
  ensure it exposes `project.version` (or edit the guard call after
  rendering).
