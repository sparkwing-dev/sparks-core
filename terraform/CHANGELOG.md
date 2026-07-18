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
- `Config.LockTimeout` sets the `-lock-timeout` threaded onto init, plan,
  and apply, defaulting to `DefaultLockTimeout` (`5m`) rather than
  terraform's fail-fast `0s`. Set `0s` to opt back into fail-fast.
- `Config.InitArgs`, `Config.PlanArgs`, and `Config.ApplyArgs` are
  passthrough slices appended to the respective terraform invocation
  (after the fixed flags; before the saved plan file for apply), so
  callers can supply `-target`, `-replace`, `-refresh=false`, `-upgrade`,
  `-parallelism`, `plan -destroy`, and the rest of terraform's option tail
  without forking the module.
- `Config.CreateWorkspace` selects the workspace with `-or-create=true`,
  creating it on first use instead of erroring when it does not yet exist.
- Apply's `SPARKWING_DRY_RUN` echo now also prints the `terraform
  workspace select` argv when `Config.Workspace` is set, so the dry-run
  output reflects every command a live apply would run.
