# terraform-plan-pr

Run `terraform init` + `terraform plan` against a Terraform root and surface
the add/change/destroy summary as a run annotation and an optional webhook
comment. The pipeline never applies: plan is the whole point, so a reviewer
sees exactly what a merge would do before anyone touches infrastructure. One
`terraform` CLI serves both AWS and GCP, so the template is cloud-agnostic.

## Scaffold

```sh
sparkwing pipeline new --name tf-plan --template terraform-plan-pr \
  --param tf-dir=infra --param var-file=prod.tfvars \
  --param notify-webhook=https://hooks.example.com/services/T000/B000/XXXX
```

## What it does

A two-node DAG:

1. `plan` runs `terraform init` and `terraform plan` in `tf-dir`, parses the
   `Plan: N to add, N to change, N to destroy` line, and records it as a run
   annotation. `plan` performs no cloud mutation, so it is safe to run on
   every pull request.
2. `report` POSTs the summary to `notify-webhook` (a Slack-style
   `{"text": ...}` payload). It `Needs` `plan`, so it runs only after the
   plan succeeds, and an empty `notify-webhook` is a safe no-op that keeps
   the node green with no webhook configured.

Splitting `report` from `plan` keeps a flaky webhook from turning a
successful plan red: the plan result stands on its own, and delivery of the
comment is a separate node.

### Never applies

There is no apply step and no rollback: plan reads state and writes a local
plan file but changes nothing in the cloud. When you want to apply the exact
reviewed plan behind a human approval gate, reach for `terraform-apply-gated`
instead -- it applies the saved plan file (not a fresh, possibly-drifted
re-plan).

### Var files and workspaces

- `var-file` is passed to plan as `-var-file`. Empty omits it. Point it at a
  per-environment tfvars file (`prod.tfvars`) to plan that environment.
- `workspace`, when set, is selected with `terraform workspace select` before
  planning. The workspace must already exist. Empty uses the current/default
  workspace.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `tf-plan` | pipeline registration name |
| `tf-dir` | no | `.` | Terraform root directory, relative to the repo root |
| `var-file` | no | `` | optional tfvars file passed as `-var-file`; empty omits it |
| `workspace` | no | `` | optional workspace selected before planning; empty uses the current one |
| `notify-webhook` | no | `` | optional webhook URL the summary is POSTed to; empty is a no-op |

## When to use

Pick for plan-only visibility into Terraform changes on a pull request, with
zero mutation. Pick `terraform-apply-gated` instead when you also want to
apply the plan behind a human approval gate. Pick `approval-gated-deploy`
instead when the gated artifact is a generic deploy rather than Terraform.

## Notes

- `terraform` must be on PATH.
- State-backend access (credentials, backend config) comes from the ambient
  environment. Plan needs read access to state; it never mutates
  infrastructure.
- The summary counts come from parsing `-no-color` plan output. A minimal
  providerless root (for example a single `null_resource`) plans green with no
  cloud credentials, so you can exercise the pipeline before wiring a real
  backend. An empty directory has no configuration and `terraform plan` errors,
  so point `tf-dir` at a root that holds at least one `.tf` file.
- The webhook payload is the Slack incoming-webhook shape. Point
  `notify-webhook` at any endpoint that accepts a `{"text": ...}` JSON body.
