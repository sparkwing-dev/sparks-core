# scheduled-cleanup

A scheduled maintenance pipeline: delete files under a directory older
than N days. Fully local. A concrete starter for the "run something on
a cron" shape.

## Scaffold

```sh
sparkwing pipeline new --name scheduled-cleanup --template scheduled-cleanup \
  --param target-dir=tmp --param max-age-days=30 --param schedule="0 3 * * *"
```

## Wire the schedule

`pipeline new` writes the `name` + `entrypoint` to
`.sparkwing/sparkwing.yaml`; add the cron trigger yourself so the
controller fires it on schedule:

```yaml
pipelines:
  - name: scheduled-cleanup
    entrypoint: ScheduledCleanup
    on:
      schedule: "0 3 * * *"
```

You can still run it on demand with `sparkwing run scheduled-cleanup`.

## What it does

One `prune` Job: walks `target-dir` (relative to the repo root) and
deletes regular files whose mtime is older than `max-age-days`. A
missing directory is a no-op, not an error. Edit the body into whatever
scheduled work you need (reporting, rotation, syncs).

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `scheduled-cleanup` | pipeline registration name |
| `schedule` | no | `0 3 * * *` | cron for the trigger (informational; wire it in yaml) |
| `target-dir` | no | `tmp` | directory to prune, repo-root-relative |
| `max-age-days` | no | `30` | delete regular files older than this |
