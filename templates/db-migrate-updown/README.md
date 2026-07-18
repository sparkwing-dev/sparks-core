# db-migrate-updown

Run [golang-migrate](https://github.com/golang-migrate/migrate) as its
own pipeline, decoupled from any build or deploy. Apply pending **up**
migrations, or roll back a chosen number of **down** steps. The target
database DSN is read from a sparkwing secret at run time, so it never
lands in the pipeline source. When the direction is `down`, a human
approval gate guards the destructive rollback.

This is a **raw-composition** template: the generated `Plan()` wires the
`migrate` block (and, on the down path, `sparkwing.JobApproval`) directly,
so you can see and edit the orchestration. The direction and the approval
gate are fixed when the template renders, so the emitted `Plan` is static
and branch-free.

## Scaffold

Forward path (apply pending migrations):

```sh
sparkwing pipeline new --name db-migrate --template db-migrate-updown \
  --param migrations-dir=db/migrations --param dsn-secret=DATABASE_URL
```

Rollback path (down one step, gated by an approval):

```sh
sparkwing pipeline new --name db-rollback --template db-migrate-updown \
  --param direction=down --param down-steps=1 --param require-approval=true
```

## When to use

- You want to run migrations standalone -- an ad-hoc schema bump,
  catching staging up to head, or an emergency down-rollback -- without
  rebuilding or redeploying the app.
- You want the destructive down path to pause for a human "go" before it
  drops schema.

## When not to use

- Migrations belong inside a full build + deploy DAG (tests on an
  ephemeral database, then a gitops/ArgoCD deploy): use
  `go-test-migrate-deploy-argo`.
- You want a restorable snapshot taken before migrating: compose this
  with `db-backup-restore-drill`'s `dbbackup` block.

## Parameters

| Parameter | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `db-migrate` | Name users type after `sparkwing run` |
| `migrations-dir` | no | `db/migrations` | golang-migrate migrations directory |
| `dsn-secret` | no | `DATABASE_URL` | sparkwing secret holding the target DB DSN |
| `direction` | no | `up` | `up` applies pending migrations, `down` rolls back (render-time branch) |
| `down-steps` | no | `1` | On the down path, how many migrations to roll back (`0` = all) |
| `require-approval` | no | `true` | On the down path, insert a human approval gate first |

`direction`, `down-steps`, and `require-approval` are consumed at render
time: they shape which nodes the template emits, not runtime behavior.
Re-render (or edit the generated file) to switch between up and down.

## After rendering

- The migrate node resolves the DSN from the `dsn-secret` secret. Set
  that secret in your runner config (for example
  `sparkwing secret set DATABASE_URL 'postgres://...'`) so it never lands
  in source.
- The `migrations-dir` must exist in the repo in golang-migrate layout
  (`NNNN_name.up.sql` / `NNNN_name.down.sql`).
- On the down path with `require-approval=true`, the run blocks at
  `approve-rollback` until a person approves. A local `sparkwing run`
  blocks in the foreground; approve from a second terminal:

  ```sh
  sparkwing runs approvals                 # find the pending run id + node
  sparkwing runs approvals approve --run <run-id> --node approve-rollback
  ```

  An unanswered gate never times out (it waits for a human) and, if it
  did expire, would resolve as denied so the rollback never runs.

## Requirements

The runner needs the `migrate` CLI
(https://github.com/golang-migrate/migrate) on PATH and network reach to
the target database. Pointed at a Postgres in Docker with a real
migrations directory, the up/down cycle runs green with no cloud
credentials.
