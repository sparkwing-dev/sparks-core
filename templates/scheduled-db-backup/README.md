# scheduled-db-backup

A cron-scheduled database backup: dump the database behind a sparkwing
secret DSN, gzip it, and upload a timestamped `<db>-<timestamp>.sql.gz`
object to durable storage. One dump-and-upload Job intended to run on a
schedule. AWS (`s3://`) and GCP (`gs://`) are first-class peers behind a
single `backup-dest` param; a plain local directory works too.

## When to use

Reach for this for periodic offsite database backups on AWS or GCP. It
produces a real artifact and ships it to object storage, so:

- Pick it over `scheduled-cleanup`, which only prunes local files and
  stores nothing.
- Pick it over `db-backup-restore-drill` when you only need to produce
  and store the dump. The drill goes further: it restores the dump into a
  throwaway instance and runs a verification query to prove the backup is
  usable.

## Scaffold

```sh
sparkwing pipeline new --name db-backup --template scheduled-db-backup \
  --param backup-dest=s3://my-db-backups/nightly \
  --param engine=postgres --param db-secret=DATABASE_URL
```

For GCP, point `backup-dest` at a bucket and set `project`:

```sh
sparkwing pipeline new --name db-backup --template scheduled-db-backup \
  --param backup-dest=gs://my-db-backups/nightly \
  --param project=my-gcp-project
```

## Wire the schedule

`pipeline new` writes the `name` + `entrypoint` to
`.sparkwing/sparkwing.yaml`; add the cron trigger yourself so the
controller fires it on schedule:

```yaml
pipelines:
  - name: db-backup
    entrypoint: DbBackup
    on:
      schedule: "0 4 * * *"
```

You can still run it on demand with `sparkwing run db-backup`.

## What it does

One `backup` Job:

1. Reads the source DSN from the `db-secret` sparkwing secret, so the
   connection string never lands in the pipeline source.
2. Dumps the database with `pg_dump` (postgres) or `mysqldump` (mysql)
   and gzip-compresses the SQL in-process.
3. Uploads the `<db>-<timestamp>.sql.gz` object to `backup-dest` (copied
   into a local dir, `aws s3 cp` for `s3://`, or `gcloud storage cp` for
   `gs://`), then annotates the run with the final object URI.

The upload honors `SPARKWING_DRY_RUN` (it echoes the argv instead of
uploading), but the dump itself always runs against the real database, so
a full `sparkwing run` needs a reachable database. That is why this
template is registered `compile-only`.

## Retention

This template does not delete old backups. Let the object store's
lifecycle rule expire them after `retention-days`, so retention keeps
working even when the pipeline does not run.

S3 lifecycle rule (expire objects under the prefix after 30 days):

```json
{
  "Rules": [
    {
      "ID": "expire-db-backups",
      "Filter": { "Prefix": "nightly/" },
      "Status": "Enabled",
      "Expiration": { "Days": 30 }
    }
  ]
}
```

GCS lifecycle rule (same 30-day expiry):

```json
{
  "rule": [
    {
      "action": { "type": "Delete" },
      "condition": { "age": 30 }
    }
  ]
}
```

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `backup-dest` | yes | | local dir, `s3://bucket/prefix`, or `gs://bucket/prefix` |
| `db-secret` | no | `DATABASE_URL` | sparkwing secret holding the source DSN |
| `engine` | no | `postgres` | `postgres` (pg_dump) or `mysql` (mysqldump) |
| `aws-profile` | no | (empty) | AWS profile for `s3://`; empty uses the default chain / IRSA |
| `project` | no | (empty) | GCP project for `gs://` destinations |
| `schedule` | no | `0 4 * * *` | cron for the trigger (informational; wire it in yaml) |
| `retention-days` | no | `30` | window the object-store lifecycle rule expires backups after |
| `pipeline-name` | no | `db-backup` | pipeline registration name |

## What to edit after rendering

- Swap `engine` and the DSN secret for your database.
- Add the `on: schedule:` trigger (see above); adjust the cron.
- Set the matching lifecycle rule on the bucket so old dumps expire.
- To also prove the backup restores, compose with (or switch to)
  `db-backup-restore-drill`.
