# skip-if-paths-unchanged

One job wrapped in a `.SkipIf(contentkey.Unchanged(base, paths...))`
guard. The job runs its command only when a tracked file under the
watched paths differs from a base ref; when nothing under them moved,
the node soft-skips with a recorded reason and the command never runs.

A plain `git diff --quiet <base> -- <paths>` makes the decision. Nothing
is hashed, stored, or replayed: this is the cheapest possible
"only run when my inputs changed" gate, ideal for monorepos where a
backend-only change should not pay for the frontend suite.

The predicate fails safe. When the base ref does not resolve (a fresh
clone missing `origin/main`, a shallow checkout without the merge-base),
`contentkey.Unchanged` reports changed and the job runs, so a broken base
never silently skips work.

## When to use

- You want a job to be skipped entirely when its inputs did not change
  versus a base branch (skip the frontend lint when only the backend
  moved, or skip the whole suite on a docs-only PR).
- You want a zero-cost `git diff` decision against a moving base, not a
  content-hash cache.
- You are gating a monorepo component: replicate this job per subtree,
  each with its own `watch-paths` and one `contentkey.Unchanged`
  predicate.

## When NOT to use

- You want an unchanged tree to replay a stored PASS rather than skip the
  node outright: use `cached-test-suite`, which keys on file content and
  hits across branches and rebases.
- You always want the suite to run: use `lint-test-go` or `test-shards`,
  which have no skip notion.
- Your repo has no git history, or the base ref never resolves in your CI
  checkout: the guard always falls through to running the job, so it buys
  you nothing.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `ci` | Verb users type after `sparkwing run` |
| `watch-paths` | no | `src,go.mod,go.sum` | Comma list of git pathspecs; a change under any runs the job |
| `base-ref` | no | `origin/main` | Diff base; a base that does not resolve fails safe and runs the job |
| `work-cmd` | no | `go test ./...` | Command the guarded job runs when the watched paths changed |

## How the skip is decided

`contentkey.Unchanged(base, paths...)` runs
`git diff --quiet <base> -- <paths>` against the working tree:

- exit 0 (no diff) means unchanged, so the predicate returns true and the
  node is skipped.
- exit 1 (a diff) means changed, so the predicate returns false and the
  node runs `work-cmd`.
- the base ref not resolving, or any other git error, returns false
  (run), so CI fails safe.

Only tracked files are compared, so `.gitignore`d and untracked files
never trigger a run. The paths are git pathspecs matched by git, not
shell globs.

## After rendering

- Set `base-ref` to whatever your CI exposes as the comparison point: the
  PR merge-base, `origin/main`, or `HEAD~1` for a local check.
- Point `watch-paths` at the subtree that actually feeds this job, then
  add a second scaffold (or a second `sparkwing.Job` in this file) for
  each other component so each guards on its own paths.
- Swap `work-cmd` for the real command (a lint, a build, a deploy). The
  guard is independent of what the job does.
