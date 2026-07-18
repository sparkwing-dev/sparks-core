# integration-test-with-service

Run integration tests against a throwaway service container (Postgres,
Redis, and the like) started before the tests and torn down after, even
on failure or panic, via `services.With`. Needs a Docker daemon; no
cloud or cluster.

## Scaffold

```sh
# Postgres (default)
sparkwing pipeline new --name integration-test --template integration-test-with-service

# Redis (no env needed -- clear env)
sparkwing pipeline new --name redis-it --template integration-test-with-service \
  --param service-image=redis:7-alpine --param service-port=6379 \
  --param ready-cmd="redis-cli ping" --param env="" \
  --param test-cmd="go test ./integration/..."
```

## What it does

One `integration` Job whose body calls `services.With`:

1. Starts `service-image`, publishing container port `service-port` to
   an ephemeral host port on `127.0.0.1`.
2. Polls `ready-cmd` **inside the container** (`docker exec`) until it
   exits 0, so the probe needs no host-side client. Bounded by
   `ready-timeout`.
3. Injects the ephemeral host port into the test as the `port-env` env
   var (default `SERVICE_PORT`) and runs `test-cmd` on the host. The
   test reads that variable to reach the service at
   `localhost:$SERVICE_PORT`.
4. Tears the container down on the way out, including on failure/panic.

This is the blessed setup/teardown idiom. Don't use `AfterRun` (an
observer that can't guarantee cleanup) or a downstream `Needs` node
(skipped when the upstream fails).

## Connecting the test to the service

The host port is chosen dynamically, so the test can't hardcode it. It's
handed to the test through `port-env`. Credentials come from `env`, and
the test must use the same ones. With the defaults, the service is
Postgres with `POSTGRES_PASSWORD=postgres` and `POSTGRES_DB=app`,
reachable at:

```
postgres://postgres:postgres@localhost:$SERVICE_PORT/app
```

Keep `env` and the DSN your test builds in sync: change the password or
database in `env` and the test's connection string has to match.

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `integration-test` | pipeline registration name |
| `service-image` | no | `postgres:16-alpine` | dependency image |
| `service-port` | no | `5432` | port the service listens on inside the container |
| `ready-cmd` | no | `pg_isready -U postgres` | readiness probe run inside the container |
| `ready-timeout` | no | `30s` | max wait for the readiness probe (Go duration) |
| `env` | no | `POSTGRES_PASSWORD=postgres,POSTGRES_DB=app` | container env as `KEY=VAL,KEY=VAL` (postgres needs a password; clear for redis) |
| `port-env` | no | `SERVICE_PORT` | env var the host port is injected into for the test |
| `test-cmd` | no | `go test ./integration/...` | test command, run on the host |

## Notes

- The host port is ephemeral and reachable at `localhost` on Linux,
  macOS, and Windows; the test learns it from `$SERVICE_PORT`.
- Raise `ready-timeout` for heavier services (a large MySQL init,
  Elasticsearch, or an image that runs migrations on boot) that take
  longer than the default to accept connections.
- One service per scaffold. To bring up more than one dependency, nest
  additional `services.With` calls inside the callback in the generated
  body.
- A hung suite runs unbounded; bound it with `.Timeout` on the
  `integration` Job. Transient image-pull failures are a candidate for a
  job-level `.Retry`.
- The test code is its own Go module at the repo root, separate from
  `.sparkwing/`.
