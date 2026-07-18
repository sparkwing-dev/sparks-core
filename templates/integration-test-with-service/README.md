# integration-test-with-service

Run integration tests against a throwaway service container (Postgres,
Redis, and the like) started before the tests and torn down after, even
on failure or panic, via `services.WithServices`. Needs a Docker daemon;
no cloud or cluster.

## Scaffold

```sh
# Postgres (default)
sparkwing pipeline new --name integration-test --template integration-test-with-service

# Redis (no env needed -- clear service-env)
sparkwing pipeline new --name redis-it --template integration-test-with-service \
  --param service-image=redis:7-alpine --param service-port=6379 \
  --param ready-cmd="redis-cli ping" --param service-env="" \
  --param test-cmd="go test ./integration/..."
```

## What it does

One `integration` Job whose body calls `services.WithServices`:

1. Starts `service-image`, published to `127.0.0.1:<service-port>`.
2. Polls `ready-cmd` **inside the container** (`docker exec`) until it
   exits 0, so the probe needs no host-side client.
3. Runs `test-cmd` on the host. Reach the service at
   `localhost:<service-port>`.
4. Tears the container down on the way out, including on failure/panic.

This is the blessed setup/teardown idiom. Don't use `AfterRun` (an
observer that can't guarantee cleanup) or a downstream `Needs` node
(skipped when the upstream fails).

## Parameters

| name | required | default | description |
|------|----------|---------|-------------|
| `pipeline-name` | no | `integration-test` | pipeline registration name |
| `service-image` | no | `postgres:16-alpine` | dependency image |
| `service-port` | no | `5432` | published to `127.0.0.1:<port>` |
| `ready-cmd` | no | `pg_isready -U postgres` | readiness probe run inside the container |
| `service-env` | no | `POSTGRES_PASSWORD=postgres,POSTGRES_DB=app` | container env as `KEY=VAL,KEY=VAL` (postgres needs a password; clear for redis) |
| `test-cmd` | no | `go test ./integration/...` | test command, run on the host |

## Notes

- Port publishing (reachable at `localhost:<port>` on macOS/Windows
  too) requires sparkwing **v0.8.1+**.
- The test code is its own Go module at the repo root, separate from
  `.sparkwing/`.
