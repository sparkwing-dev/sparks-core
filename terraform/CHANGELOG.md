# Changelog: terraform

All notable changes to the **`github.com/sparkwing-dev/sparks-core/terraform`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `terraform/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added
- Initial release. `Plan` runs `terraform init`, optionally selects a
  workspace, then plans to a saved plan file and parses the
  add/change/destroy summary into a `PlanResult`. `Apply` applies exactly
  that saved plan (`terraform apply <planfile>`) and never re-plans, so
  what runs is what a reviewer approved. Cloud-agnostic: one `terraform`
  binary serves AWS and GCP. Requires `terraform` on PATH.
- `Plan` is state-reading and always executes; `Apply` is
  cloud-mutating and honors `SPARKWING_DRY_RUN`, echoing the exact
  terraform argv and returning success without applying when the variable
  is set.
- `ParseChangeSummary` extracts the counts from `terraform plan` output
  (the `Plan: N to add, N to change, N to destroy.` line and the
  `No changes.` message).
