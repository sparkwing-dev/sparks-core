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
  - Cloud uploads honor `SPARKWING_DRY_RUN`: when it is set they echo
    the exact command argv and return success without executing. Local
    dump / restore and cloud downloads read or produce local state and
    always run.
  - Supports the postgres (pg_dump / psql) and mysql (mysqldump / mysql)
    engines. Requires the matching host binaries, plus the `aws` CLI for
    `s3://` and the `gcloud` CLI for `gs://` destinations.
