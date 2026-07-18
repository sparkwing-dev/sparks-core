# approval-gated-deploy

`build -> test -> human approval gate -> deploy`. The deploy node runs
only after a person approves the gate. Fully local to run; the build,
test, and deploy steps are placeholder commands you replace.

## Scaffold

```sh
sparkwing pipeline new --name gated-deploy --template approval-gated-deploy \
  --param environment=prod --param build-cmd='go build ./...' \
  --param test-cmd='go test ./...' \
  --param deploy-cmd='./bin/deploy.sh prod' --param timeout-hours=2
```

## What it does

`build` runs `build-cmd` and `test` runs `test-cmd`, then
`sparkwing.JobApproval` pauses the run at `approve-deploy`. `deploy`
`Needs` the gate, so it runs `deploy-cmd` only once the gate is approved.
If the gate isn't answered within `timeout-hours` it expires as
**denied** (deploy never runs). A `timeout-hours` of `0` disables the
timeout, so the gate waits indefinitely.

This template provides the approval gate, not deploy verification or
rollback. `deploy` runs your command and reports its success or failure,
but adds no post-deploy health check and no automatic rollback. Add a
`Verify` probe and an `OnFailure` handler to the `deploy` job if the
target needs them.

## Approving a run

Local `sparkwing run` blocks in the foreground at the gate. Approve
from a second terminal (or the dashboard):

```sh
sparkwing runs approvals                 # find the pending run id + node
sparkwing runs approvals approve --run <run-id> --node approve-deploy
```

(Keep the original `run` process alive while you approve; closing it
strands the run.)

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `gated-deploy` | pipeline registration name |
| `environment` | no | `prod` | shown in the approval prompt |
| `build-cmd` | no | `echo "build"` | command the build step runs |
| `test-cmd` | no | `echo "test"` | command the test step runs |
| `deploy-cmd` | no | `echo "deploying"` | command the deploy step runs after approval |
| `timeout-hours` | no | `2` | hours the gate waits before expiring as denied; `0` waits indefinitely |
