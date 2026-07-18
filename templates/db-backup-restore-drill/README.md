# db-backup-restore-drill

A disaster-recovery drill: dump the source Postgres database, restore
that dump into a throwaway ephemeral instance, and run a verification
query against the restore to prove the backup is actually usable. Reports
pass/fail through an optional webhook. Intended to run on a schedule.
Needs a Docker daemon plus `pg_dump` and `psql`; no cloud or cluster.

This is the only starter that exercises the restore path. A dump you have
never restored is a backup you cannot trust: this drill catches silent
corruption or a broken dump before a real outage, not during one.

## Scaffold

```sh
# Drill an ephemeral seeded Postgres source (no setup, runs anywhere)
sparkwing pipeline new --name backup-drill --template db-backup-restore-drill

# Drill a real Postgres read from a sparkwing secret, asserting rows survive
sparkwing pipeline new --name backup-drill --template db-backup-restore-drill \
  --param dsn-secret=DATABASE_URL \
  --param verify-query="SELECT count(*) FROM users" \
  --param verify-min-rows=1 \
  --param notify-webhook-secret=DRILL_WEBHOOK_URL
```

## Wire the schedule

`pipeline new` writes the `name` + `entrypoint` to
`.sparkwing/sparkwing.yaml`; add the cron trigger yourself so the
controller fires the drill on schedule:

```yaml
pipelines:
  - name: backup-drill
    entrypoint: BackupDrill
    on:
      schedule: "0 6 * * *"
```

You can still run it on demand with `sparkwing run backup-drill`.

## What it does

One `drill` Job, bounded by `timeout`:

1. Resolves the source DSN. When `dsn-secret` is configured it dumps that
   real Postgres database and asserts your `verify-query`; when it is not,
   the drill stands up an ephemeral Postgres, seeds it with a small table,
   and asserts those seeded rows survive -- so the pipeline runs with no
   setup and still proves a real round trip.
2. Stands up a throwaway ephemeral Postgres as the restore target. The
   target is always ephemeral, so the drill never writes to a real
   database.
3. `dbbackup.Dump` runs `pg_dump` against the source and writes a
   compressed `.sql.gz` to `backup-dest` (a local dir, `s3://`, or
   `gs://`); an empty `backup-dest` uses a run-local temp dir that is
   removed afterward.
4. `dbbackup.Restore` replays that dump into the throwaway target.
5. `dbbackup.VerifyRestore` runs the verify query against the restore.
   With `verify-min-rows` above 0 it asserts the first cell is a count at
   least that large; at 0 any error-free result passes.
6. The result (pass or fail), including a failure to provision the
   ephemeral instances, is annotated on the run and posted to the webhook
   named by `notify-webhook-secret` when one is set.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `backup-drill` | pipeline registration name |
| `dsn-secret` | no | `DATABASE_URL` | sparkwing secret holding the source Postgres DSN; unconfigured falls back to an ephemeral seeded source |
| `backup-dest` | no | (empty) | where the dump is written: local dir, `s3://bucket/prefix`, or `gs://bucket/prefix`; empty uses a temp dir |
| `aws-profile` | no | (empty) | AWS profile for an `s3://` backup-dest; empty uses the default credential chain |
| `project` | no | (empty) | GCP project ID for a `gs://` backup-dest |
| `verify-query` | no | `SELECT 1` | SQL asserted against the restore on the real-source path |
| `verify-min-rows` | no | `0` | when above 0, require verify-query's first cell to be a count at least this large |
| `timeout` | no | `30m` | max wall-clock for the drill Job (Go duration) |
| `schedule` | no | `0 6 * * *` | cron for the trigger (informational; wire it in yaml) |
| `notify-webhook-secret` | no | (empty) | sparkwing secret holding a webhook URL for the pass/fail result; empty skips it |

## Notes

- Postgres only: the source and restore instances the drill provisions
  are Postgres, so it needs `pg_dump` and `psql` on PATH. `dbbackup` also
  supports MySQL, but driving a MySQL drill means supplying your own
  MySQL source and throwaway restore target in the body.
- An `s3://` `backup-dest` needs the `aws` CLI (`aws-profile` selects the
  profile); a `gs://` one needs `gcloud` (`project` selects the GCP
  project). Both upload for real. To keep a scheduled drill
  self-contained, leave `backup-dest` empty (temp dir) or point it at a
  local directory.
- The webhook URL is read from a sparkwing secret at run time, so the
  token embedded in a Slack/Discord incoming-webhook URL never lands in
  your committed source.
- To turn this into a snapshot-then-migrate safety net, call
  `dbbackup.Dump` before a risky migration and wire the returned
  artifact through `dbbackup.RestoreFunc(...)` as the migration Job's
  `OnFailure` handler.
