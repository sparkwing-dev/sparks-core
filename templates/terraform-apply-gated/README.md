# terraform-apply-gated

`terraform plan -> human approval gate -> terraform apply`. The plan
runs and reports its add/change/destroy summary, the run blocks at a
`sparkwing.JobApproval`, and only after a person approves does it apply
that exact saved plan file. An optional Slack announce reports the result.

## When to use

Reach for this to ship Terraform changes that must pause for a human
"go" after the plan is visible and before anything mutates:

- Pick **terraform-plan-pr** instead when you only want plan visibility
  on a pull request and never apply from the pipeline.
- Pick **approval-gated-deploy** instead when the gated artifact is a
  generic deploy command, not Terraform. This template is
  Terraform-aware: it applies the exact saved plan the reviewer saw, not
  a fresh plan taken after approval that could have drifted.

## Scaffold

```sh
sparkwing pipeline new --name tf-apply --template terraform-apply-gated \
  --param tf-dir=infra --param environment=prod \
  --param var-file=common.tfvars,prod.tfvars --param workspace=prod \
  --param backend-config=bucket=my-tf-state,key=prod/terraform.tfstate,region=us-west-2 \
  --param slack-webhook-secret=SLACK_WEBHOOK_URL
```

## What it does

1. `plan` runs `terraform init` + `terraform plan -out=tfplan` and
   annotates the run with the `Plan: N to add, N to change, N to
   destroy` summary.
2. `sparkwing.JobApproval` pauses the run at `approve-apply`. If the gate
   is not answered within `timeout-hours` it expires as **denied** and
   apply never runs.
3. `apply` `Needs` the gate, so it runs only once approved. It applies
   the exact `tfplan` file from step 1 (never a re-plan), which closes
   the drift window between what the reviewer approved and what runs.
4. On a live apply, the outcome is announced to Slack when
   `slack-webhook-secret` names a sparkwing secret holding a webhook URL;
   an unset secret or a dry run skips the announce.

`terraform apply` mutates real infrastructure, so a scaffold of this
template is verified compile-only. The `apply` step honors
`SPARKWING_DRY_RUN`: export it and the step echoes the `terraform apply`
argv instead of mutating state, which is handy for a first local dry run.

## Per-environment backend and variables

- `backend-config` passes partial backend settings to `terraform init` as
  `-backend-config KEY=VAL` (in sorted-key order), so distinct
  environments can point at their own state bucket, key, region, or lock
  table without editing the generated code.
- `var-file` accepts a comma-separated list applied in order, so a shared
  `common.tfvars` can layer under a per-environment `prod.tfvars`.
- `vars` passes individual `-var KEY=VAL` pairs for values threaded in
  from an upstream step (for example an image tag) that do not live in a
  committed tfvars file.

## Approving a run

A local `sparkwing run` blocks in the foreground at the gate. Approve
from a second terminal (or the dashboard):

```sh
sparkwing runs approvals                 # find the pending run id + node
sparkwing runs approvals approve --run <run-id> --node approve-apply
```

Keep the original `run` process alive while you approve; closing it
strands the run.

## Prerequisites

- A Terraform root (the `.tf` files and backend config) at `tf-dir`.
- The `terraform` CLI on `PATH`.
- Backend credentials: read access for plan, write access for apply.
  State-backend access comes from the ambient environment.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `tf-apply` | pipeline registration name |
| `tf-dir` | no | `.` | Terraform root directory |
| `var-file` | no | `` | comma-separated tfvars files passed as `-var-file`, in order; empty omits them |
| `vars` | no | `` | individual plan variables as `KEY=VAL,KEY=VAL`, passed as `-var`; empty passes none |
| `backend-config` | no | `` | partial backend settings as `KEY=VAL,KEY=VAL`, passed to init as `-backend-config`; empty uses the backend block as written |
| `workspace` | no | `` | terraform workspace to select before planning; empty uses current |
| `environment` | no | `prod` | environment name shown in the approval prompt |
| `timeout-hours` | no | `2` | whole number of hours the gate waits before expiring as denied |
| `slack-webhook-secret` | no | `` | sparkwing secret holding a Slack webhook URL to announce the apply result; empty skips it |

## After rendering

- Point `tf-dir` at your Terraform root and confirm the backend is
  configured there.
- Wire real backend credentials into the runner environment before a
  live apply.
- The apply notification uses the `{"text": ...}` Slack shape; adjust the
  `notify.Slack` call if your webhook expects a different payload.
