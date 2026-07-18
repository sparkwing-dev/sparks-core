# terraform-apply-gated

`terraform plan -> human approval gate -> terraform apply`. The plan
runs and reports its add/change/destroy summary, the run blocks at a
`sparkwing.JobApproval`, and only after a person approves does it apply
that exact saved plan file. An optional webhook announces the result.

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
  --param var-file=prod.tfvars --param workspace=prod \
  --param notify-webhook="$SLACK_WEBHOOK_URL"
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
4. The apply outcome is POSTed to `notify-webhook` when set; an empty URL
   is a no-op.

`terraform apply` mutates real infrastructure, so a scaffold of this
template is verified compile-only. The `apply` step honors
`SPARKWING_DRY_RUN`: export it and the step echoes the `terraform apply`
argv instead of mutating state, which is handy for a first local dry run.

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
| `var-file` | no | `` | tfvars file passed to plan as `-var-file`; empty omits it |
| `workspace` | no | `` | terraform workspace to select before planning; empty uses current |
| `environment` | no | `prod` | environment name shown in the approval prompt |
| `timeout-hours` | no | `2` | hours the gate waits before expiring as denied |
| `notify-webhook` | no | `` | Slack-style webhook the apply result is POSTed to; empty skips it |

## After rendering

- Point `tf-dir` at your Terraform root and confirm the backend is
  configured there.
- Wire real backend credentials into the runner environment before a
  live apply.
- The apply notification uses the `{"text": ...}` Slack shape; adjust the
  `notify.Slack` call if your webhook expects a different payload.
