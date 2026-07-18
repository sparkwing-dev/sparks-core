# lint-test-python

CI hygiene gate for a Python project on the uv/ruff/pytest stack. An
install node gates a parallel fan-out of ruff format, ruff check, mypy,
and pytest, so a single run surfaces every failure at once. No cloud, no
registry, no cluster. The Python twin of lint-test-go.

Every check is a string parameter. The defaults target uv (the house
toolchain); swap them for pip or poetry forms if that is your setup, or
leave one empty to drop that node from the DAG.

## When to use

- CI hygiene for a Python repo: format, lint, typecheck, and test on
  every push or pre-commit, with no build or deploy.
- You want one run to report all failures, not stop at the first.
- You are scaffolding a fresh Python service and want something
  compilable to iterate from.

## When NOT to use

- The project is Go or Node. Use lint-test-go or lint-test-node.
- Your suite needs a live dependency (Postgres, Redis) running
  alongside it. Use integration-test-with-service.
- You actually want to build or deploy something. Pick a build/deploy
  template instead.

## The DAG

`install` runs first. When it succeeds, `format`, `lint`, `typecheck`,
and `test` run in parallel, each depending only on `install` and never
on each other. Leaving `install-cmd` empty drops the install node and
lets the checks run as independent roots. Leaving any check command
empty drops just that node.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `lint-test-python` | Verb users type after `sparkwing run` |
| `python-version` | no | `3.12` | Banner version (real version is pinned in pyproject/uv) |
| `install-cmd` | no | `uv sync --frozen` | Environment install run before the checks; empty to skip |
| `format-cmd` | no | `uv run ruff format --check .` | Formatter check; empty disables the node |
| `lint-cmd` | no | `uv run ruff check .` | Lint command (ruff); empty disables the node |
| `typecheck-cmd` | no | `uv run mypy .` | Type-check command (mypy/pyright); empty disables the node. `.` can mis-scope on a src/ layout -- point at your package (`uv run mypy src`) if so |
| `test-cmd` | no | `uv run pytest` | Test command; empty disables the node |

mypy against `.` is not always the right target: on a src/ layout or a
repo with several test directories it can raise spurious duplicate-module
errors. ruff tolerates `.` (it ships a broad default-exclude list); mypy
does not. Point `typecheck-cmd` at your package (`uv run mypy src`) if
that bites.

## Scaffold

```sh
sparkwing pipeline new --name lint-test-python --template lint-test-python
```

## After rendering

Point the commands at your real toolchain if you are not on uv. For a
pip project, set `install-cmd` to `pip install -e '.[dev]'` and drop the
`uv run` prefix from the check commands. To add a security or dependency
scan, add another `sparkwing.Job` node that Needs the install job.

A few deliberate omissions, in case you want them:

- **Caching.** The install node is the slowest, most-cacheable step, but
  this template stays minimal and does not cache. For a resolved
  environment keyed on the lockfile, use `cached-test-suite` (whole-job
  content replay) as the model, or add a `.Cache(...)` on the install
  node keyed on `uv.lock`/`pyproject.toml`.
- **Install retry/timeout.** `install-cmd` does network I/O (package
  downloads) and is not retried here. If registry flakes bite, add
  `.Retry(...)` and/or `.Timeout(...)` on the install node.
- **Dropping install with the uv defaults.** Each default check is a
  `uv run ...`, and `uv run` auto-provisions `.venv` on first use. The
  install node exists partly to serialize that. Leaving `install-cmd`
  empty is safe only when the check commands do not each auto-provision
  the environment (a non-uv toolchain, or a pre-provisioned venv);
  otherwise the four parallel `uv run` invocations can race on `.venv`
  creation.
