# Contributor orientation

This repository contains independently versioned Go modules. Read `README.md`
for module boundaries, dependency rules and release policy. `spark.json` owns
the module list.

- Run checks from the affected module. `go test ./...` at the repository root
  does not cover nested modules. Use `GOWORK=off go -C <module> test ./...` to
  test published dependencies. For changes spanning modules, create a local
  `go.work` containing the affected modules and their local dependencies.
- Keep module `go.mod` files on published dependencies. The workspace is the
  local override and stays uncommitted.
- Before editing templates, read `templates/README.md`. Verify rendered Go,
  including empty optional commands, and update `templates/CHANGELOG.md` when
  generated behavior or help changes.
- For command helpers, follow `step`: pass the caller's context and return
  command errors. When adding operation context, preserve the underlying error
  with `%w` so callers can inspect it. Read the receiving module's tests before
  changing its error contract.
- Discover repository checks with `sparkwing info --for-agent` and
  `sparkwing pipeline list -o json`. Run the relevant pipeline with
  `sparkwing run <name>`. Record focused checks and review before landing.
  Releases are separate work and follow the README's release policy.
