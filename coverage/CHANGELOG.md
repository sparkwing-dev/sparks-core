# Changelog: coverage

All notable changes to the **`github.com/sparkwing-dev/sparks-core/coverage`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `coverage/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. Parses total coverage from three report formats
  behind one `Report{Format, Path}` type: Go coverprofiles (the
  statement-weighted total that `go tool cover -func` prints), lcov
  tracefiles (hit lines over found lines, LH/LF), and Cobertura XML
  (the root line-rate, with a lines-covered/lines-valid fallback).
- `Total` returns the parsed percentage; `GateAtLeast` returns a
  Verify-shaped `func(context.Context) error` that logs the measured
  total and fails with a rich error naming the total, the floor, and
  the report when coverage falls below the threshold. Parsing is pure,
  with no host tools or network, so one gate covers Go, Node, Python,
  and Rust suites.
