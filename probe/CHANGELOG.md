# Changelog: probe

All notable changes to the **`github.com/sparkwing-dev/sparks-core/probe`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `probe/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. `probe.HTTP(url)` builds an HTTP health probe whose
  `Check` method is a `func(ctx) error` suitable for sparkwing's
  `Job.Verify`. Supports custom method, static and per-attempt
  (`HeaderFunc`) headers, request body, `ExpectStatus`, `ExpectJSON`
  (dotted-path assertions), and retry/interval/timeout. `Indeterminate`
  classifies a failure as "could not determine health" (transport, auth,
  timeout, undecodable body) versus a definitive unhealthy response, so
  recovery logic can escalate on the former and roll back on the latter.
  Stdlib-only; does not import the sparkwing SDK.
