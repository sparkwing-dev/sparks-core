# Changelog: dbbackup

All notable changes to the **`github.com/sparkwing-dev/sparks-core/dbbackup`** module
are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/);
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Tagging convention: `dbbackup/vMAJOR.MINOR.PATCH` (per Go module
multi-module repo conventions).

## [Unreleased]

### Added

- Initial release. Database dump / upload / restore / verify helpers
  shared by the scheduled-db-backup and db-backup-restore-drill
  templates.
  - `Dump` runs pg_dump or mysqldump, gzip-compresses the SQL in
    process, and delivers the `<db>-<timestamp>.sql.gz` artifact to a
    local directory, an `s3://` prefix (via `aws s3 cp`), or a `gs://`
    prefix (via `gcloud storage cp`). Returns an `Artifact` handle with
    the final URI and compressed size.
  - `Restore` pulls a source dump (local, `s3://`, or `gs://`),
    decompresses it, and replays it into a target DSN with psql or the
    mysql client. Shaped as `func(ctx) error` for a Job body or an
    OnFailure handler.
  - `RestoreFunc` turns a prior `Dump` artifact into an OnFailure-shaped
    rollback closure, for the snapshot-then-migrate safety net.
  - `VerifyRestore` runs a verification query (smoke check, or a
    row-count assertion via `MinRows`) against a restored database for
    restore drills.
  - Mutating operations honor `SPARKWING_DRY_RUN`: the `s3://` / `gs://`
    uploads and the restore replay into the target database echo the
    exact command and return success without executing. The dump
    (read-only against the source) and cloud downloads read or produce
    local state and always run.
  - `Config.DumpArgs` / `Config.RestoreArgs` pass extra flags through to
    pg_dump / mysqldump and psql / mysql (for example `--schema-only`,
    `--exclude-table`, or a `--set` var).
  - mysqldump defaults to `--single-transaction` for a consistent InnoDB
    dump without table locks.
  - Object-store uploads and downloads retry with bounded exponential
    backoff so a single transient `s3://` / `gs://` blip does not fail a
    whole backup or restore; the local dump and restore stay single-shot.
  - Postgres credentials travel via `PGPASSWORD` rather than on the argv,
    so a password embedded in a libpq URI DSN stays off the process
    table, matching the mysql path's `MYSQL_PWD` handling.
  - `Artifact` records the delivery `AWSProfile` / `Project` so a
    `RestoreFunc` rollback fetches the snapshot back with the same
    credential context the `Dump` used.
  - The intermediate dump uses a private scratch path, so a custom
    `Config.Filename` without a `.gz` suffix can no longer collide with
    the compressed output; the scratch files are always cleaned up.
  - Supports the postgres (pg_dump / psql) and mysql (mysqldump / mysql)
    engines. Requires the matching host binaries, plus the `aws` CLI for
    `s3://` and the `gcloud` CLI for `gs://` destinations.
