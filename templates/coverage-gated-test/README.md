# coverage-gated-test

Run a test suite that emits a coverage report, then fail the run when
total line coverage falls below a floor. A single `test` Job runs the
test command; its `Verify` postcondition is `coverage.GateAtLeast(min,
report)`, which parses the report and fails the node when the measured
total is under the threshold.

The gate is the `coverage` block. Its parser understands three report
formats behind one `Report{Format, Path}` type, so the same template
gates a Go, Node, Python, or Rust suite:

- `go` -- a Go coverprofile (what `go test -coverprofile=cover.out`
  writes); the statement-weighted total that `go tool cover -func`
  reports.
- `lcov` -- an lcov tracefile (`.info`); hit lines over found lines
  (LH / LF) summed across records.
- `cobertura` -- Cobertura XML; the root line-rate scaled to a percent.

## When to use

- Passing tests are not enough: you want the run to FAIL when coverage
  drops below a set percentage.
- Your test command already emits (or can emit) one of the three
  supported report formats.

## When NOT to use

- You do not enforce a coverage floor. Use `lint-test-go`, which is
  pass/fail with no coverage notion.
- You want to speed up a slow suite by splitting it across parallel
  jobs. Use `test-shards`; it fans the suite out and gates on an
  all-pass node, with no coverage measurement.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `coverage-gate` | Verb users type after `sparkwing run` |
| `test-cmd` | no | `go test -coverprofile=cover.out ./...` | Test command that emits the coverage file |
| `coverage-file` | no | `cover.out` | Path (relative to the repo root) of the report the gate parses |
| `coverage-format` | no | `go` | Report format: `go`, `lcov`, or `cobertura` |
| `min-coverage` | no | `80` | Minimum total line coverage percent; the gate fails below this |

## Non-Go suites

Point `test-cmd`, `coverage-file`, and `coverage-format` at your
toolchain's coverage output:

- Node (jest / vitest, lcov): `test-cmd=npx jest --coverage`,
  `coverage-file=coverage/lcov.info`, `coverage-format=lcov`.
- Python (pytest-cov, Cobertura): `test-cmd=pytest --cov --cov-report=xml`,
  `coverage-file=coverage.xml`, `coverage-format=cobertura`.
- Rust (tarpaulin, lcov): `test-cmd=cargo tarpaulin --out Lcov`,
  `coverage-file=lcov.info`, `coverage-format=lcov`.

The gate does not run the test command for you inside the report parser;
`test-cmd` must actually write the file at `coverage-file` before the
`Verify` stage reads it.

## After rendering

- Raise `min-coverage` toward your real floor once the suite is
  measured; a floor above current coverage fails immediately, which is
  the point when ratcheting.
- The gate parses total coverage only. To fail on a per-package or
  per-file regression, add a second `Verify` or a follow-on Job that
  inspects the same report.
