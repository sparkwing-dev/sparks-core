# Changelog: notify

All notable changes to the **`github.com/sparkwing-dev/sparks-core/notify`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `notify/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. `Webhook` POSTs a JSON payload to an HTTP webhook
  (non-2xx is an error); `Slack` is the `{"text": ...}` convenience for
  Slack-compatible incoming webhooks. Shaped as func(ctx) error for use
  in a step or an OnFailure recovery handler. An empty URL is a no-op
  with a warning, so a missing webhook never fails the recovery path it
  sits in.
