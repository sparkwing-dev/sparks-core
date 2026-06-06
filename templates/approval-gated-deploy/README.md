# approval-gated-deploy

`build -> test -> human approval gate -> deploy`. The deploy node runs
only after a person approves the gate. Fully local to run; the deploy
step is a placeholder you replace.

## Scaffold

```sh
sparkwing pipeline new --name gated-deploy --template approval-gated-deploy \
  --param environment=prod --param deploy-cmd='./bin/deploy.sh prod' --param timeout-hours=2
```

## What it does

`build` and `test` run, then `sparkwing.JobApproval` pauses the run at
`approve-deploy`. `deploy` `Needs` the gate, so it only runs once the
gate is approved. If the gate isn't answered within `timeout-hours` it
expires as **denied** (deploy never runs).

## Approving a run

Local `sparkwing run` blocks in the foreground at the gate — approve
from a second terminal (or the dashboard):

```sh
sparkwing runs approvals                 # find the pending run id + node
sparkwing runs approvals approve --run <run-id> --node approve-deploy
```

(Keep the original `run` process alive while you approve — closing it
strands the run.)

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `gated-deploy` | pipeline registration name |
| `environment` | no | `prod` | shown in the approval prompt |
| `deploy-cmd` | no | `echo "deploying"` | command the deploy step runs after approval |
| `timeout-hours` | no | `2` | hours the gate waits before expiring as denied |
