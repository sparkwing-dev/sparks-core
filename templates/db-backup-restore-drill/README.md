# db-backup-restore-drill

A disaster-recovery drill: dump the source database, restore that dump
into a throwaway ephemeral instance, and run a verification query against
the restore to prove the backup is actually usable. Reports pass/fail
through an optional webhook. Intended to run on a schedule. Needs a Docker
daemon plus the engine's dump/restore client tools; no cloud or cluster.

This is the only starter that exercises the restore path. A dump you have
never restored is a backup you cannot trust: this drill catches silent
corruption or a broken dump before a real outage, not during one.

## Scaffold

```sh
# Postgres, drilling an ephemeral seeded source (no setup, runs anywhere)
sparkwing pipeline new --name backup-drill --template db-backup-restore-drill

# Drill a real database read from a sparkwing secret
sparkwing pipeline new --name backup-drill --template db-backup-restore-drill \
  --param source-dsn-secret=DATABASE_URL \
  --param verify-query="SELECT count(*) FROM users" \
  --param notify-webhook="https://hooks.example.com/backup-drill"
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

One `drill` Job:

1. Resolves the source DSN. When the `source-dsn-secret` is configured it
   dumps that real database; when it is not, the drill stands up an
   ephemeral seeded Postgres source, so the pipeline runs with no setup.
2. Stands up a throwaway ephemeral Postgres as the restore target. The
   target is always ephemeral, so the drill never writes to a real
   database.
3. `dbbackup.Dump` runs `pg_dump` (or `mysqldump`) against the source and
   writes a compressed `.sql.gz` to `backup-dest` (a local dir, `s3://`,
   or `gs://`); an empty `backup-dest` uses a run-local temp dir that is
   removed afterward.
4. `dbbackup.Restore` replays that dump into the throwaway target.
5. `dbbackup.VerifyRestore` runs `verify-query` against the restore. A
   non-error result passes the drill.
6. The result (pass or fail) is annotated on the run and posted to
   `notify-webhook` when one is set. A failed step fails the job while
   still sending the notification.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `backup-drill` | pipeline registration name |
| `source-dsn-secret` | no | `DATABASE_URL` | sparkwing secret holding the source DSN; unconfigured falls back to an ephemeral seeded source |
| `engine` | no | `postgres` | database engine: `postgres` or `mysql` |
| `backup-dest` | no | (empty) | where the dump is written: local dir, `s3://bucket/prefix`, or `gs://bucket/prefix`; empty uses a temp dir |
| `verify-query` | no | `SELECT 1` | SQL asserted against the restore; set a real row-count or smoke check |
| `schedule` | no | `0 6 * * *` | cron for the trigger (informational; wire it in yaml) |
| `notify-webhook` | no | (empty) | webhook URL posted the pass/fail result as JSON; empty skips it |

## Notes

- Requires the engine's client binaries on PATH: `pg_dump` + `psql` for
  postgres, `mysqldump` + `mysql` for mysql. An `s3://` `backup-dest`
  needs the `aws` CLI; a `gs://` one needs `gcloud`.
- The ephemeral source and restore instances are Postgres. The `engine`
  parameter selects the dump/restore/verify toolchain, so set
  `engine=mysql` only when you point `source-dsn-secret` at a real MySQL
  source and supply your own throwaway MySQL restore target in the body.
- An `s3://` or `gs://` `backup-dest` uploads for real. To keep a
  scheduled drill self-contained, leave `backup-dest` empty (temp dir) or
  point it at a local directory.
- To turn this into a snapshot-then-migrate safety net, call
  `dbbackup.Dump` before a risky migration and wire the returned
  artifact through `dbbackup.RestoreFunc(...)` as the migration Job's
  `OnFailure` handler.
