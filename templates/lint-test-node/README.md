# lint-test-node

CI hygiene for a Node/TypeScript project. Installs dependencies, then
runs lint, typecheck, and test as independent parallel nodes gated on a
successful install. No cloud, registry, or cluster concerns.

The Node twin of `lint-test-go`: same shape (install gates, checks fan
out), every command a string parameter so it fits npm, pnpm, or yarn and
eslint, tsc, vitest, jest, or `node --test`. Each check is independent,
so one run reports every failure at once instead of stopping at the
first.

## When to use

- You want a CI hygiene gate on a JS/TS repo that runs on every push or
  pre-commit.
- You want lint, typecheck, and test to run in parallel and all report,
  not short-circuit.
- You use npm, pnpm, or yarn -- point `install-cmd` at your manager.

## When NOT to use

- The project is Go (use `lint-test-go`) or Python (use
  `lint-test-python`).
- You want to build or deploy something -- pick a build/deploy template.
- You only want a check to run when its package changed in a monorepo --
  gate this pipeline with `skip-if-paths-unchanged`.

## Parameters

| Name | Required | Default | Description |
|---|---|---|---|
| `pipeline-name` | no | `lint-test-node` | Verb users type after `sparkwing run` |
| `node-version` | no | `20` | Banner version (real version pinned by your toolchain / .nvmrc) |
| `install-cmd` | no | `npm ci` | Install run before the checks; empty to skip |
| `lint-cmd` | no | `npm run lint` | Lint command (eslint); empty disables the node |
| `typecheck-cmd` | no | `npx tsc --noEmit` | Type-check command; empty disables the node (plain-JS) |
| `test-cmd` | no | `npm test` | Test command; empty disables the node |

Blank any check to drop its node. Leaving `typecheck-cmd` empty is the
plain-JS path (no TypeScript). Set `install-cmd` to
`pnpm install --frozen-lockfile` or `yarn install --immutable` for those
package managers, or blank it to skip install entirely.

## After rendering

- Add a separate node for a second linter (prettier, stylelint) by
  copying one of the check methods and wiring another
  `sparkwing.Job(...).Needs(install)` in `Plan`.
- The checks run at the repo root. For a package in a subdirectory,
  prefix the command (e.g. `npm --prefix packages/web test`) or run one
  instance of this pipeline per package.
