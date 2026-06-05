# Authoring sparks-core

sparks-core is a library of pipeline building blocks plus a registry of
templates that compose them. This page is the shape contract every
module and template follows so that a reader -- human or agent -- can
predict the surface of a module they've never opened, and assemble a
working pipeline from blocks they've never seen.

There are exactly three kinds of artifact here. Know which one you're
writing before you start.

## 1. Blocks (the vocabulary)

A block is a single capability -- build an image, run migrations, probe
a URL, roll back a deployment. Blocks are ordinary, hand-written Go in
their own module. They are always imported, never scaffolded; even a
template's generated output calls into them.

A block exposes one of two shapes:

**Config struct + function**, when the call has more than two or three
inputs:

```go
type BuildConfig struct {
    Image      string
    Dockerfile string
    Registries []string
}

func BuildAndPush(ctx context.Context, cfg BuildConfig) error
```

**Chainable builder**, when the call is option-heavy and most options
are usually left at their default:

```go
probe.HTTP("https://svc/healthz").
    ExpectJSON("status", "ok").
    Retry(30).Interval(2 * time.Second).Check
```

Both shapes terminate in a `func(ctx context.Context) error` (or a
method with that signature, like `probe`'s `Check`). That's the
contract: a block's unit of work plugs directly into a sparkwing `Step`
or `Job` body, or into a `Job.Verify` / `OnFailure` hook, with no
adapter.

Rules every block follows:

- **`func(ctx) error` at the boundary.** No panics for expected
  failures; return the error. (`step.Run` / `step.Exec` wrap the
  shell-out and propagate.)
- **Wrap the work in a `step.Run(ctx, "label", ...)` banner** so the
  log stream shows where one block's work begins.
- **Defaults live in the function, not the caller.** A zero-value field
  resolves to a sane default (`Dockerfile` -> `"Dockerfile"`,
  `Namespace` -> `"default"`). Document the default on the field.
- **Minimal dependencies.** A block depends on `step`, the sparkwing
  SDK, and the stdlib -- and only on another sparks-core block when it
  genuinely orchestrates it (`deploy` -> `gitops` + `kube`). The fewer
  cross-module edges, the less the release graph cascades.
- **Single responsibility.** `docker` builds and pushes; `kube` talks
  to kubectl; `gitops` writes the gitops repo. Don't blend concerns
  into one module.
- **Shell out to host tools the way the rest of the repo does.** Assume
  `docker`, `kubectl`, `kind`, `git` are present; document any
  additional required binary (e.g. `migrate`) in the package doc.

Each block is its own Go module: own `go.mod`, own `CHANGELOG.md`, own
`<module>/vMAJOR.MINOR.PATCH` tag. New modules start at `v0.1.0`.

## 2. Templates (the assembly)

A template is a *starter pipeline* -- a whole DAG of blocks wired
together for one common shape (build-test-deploy, test-migrate-deploy,
static-site, ...). It is code generation, not a dependency: the
sparkwing CLI's `pipeline new --template` renders it into the
consumer's `.sparkwing/jobs/`, and from then on they own the file.

Templates are **raw composition of blocks**. The generated `Plan()`
calls blocks directly so the reader sees the orchestration -- what runs
when, what gates on what, what rolls back on a failed probe -- and can
edit the one branch they care about. The heavy lifting stays in the
blocks, so a template body is short: orchestration only.

```go
func (p *BuildTestDeploy) Plan(_ context.Context, plan *sw.Plan, _ sw.NoInputs, rc sw.RunContext) error {
    test  := sw.Job(plan, "test", runTests)
    build := sw.Job(plan, "build", buildImage).Needs(test)
    sw.Job(plan, "deploy", deployApp).
        Needs(build).
        Verify(probe.HTTP("https://svc/healthz").Retry(30).Check).
        OnFailure("rollback", rollBack)
    return nil
}
```

Keep logic *out* of templates. When a block's internals change, the
template doesn't: it still just calls `docker.BuildAndPush`. A template
only changes when the *shape* of a pipeline changes -- which is rare --
so templates almost never churn.

A template is a directory with three files (see
[`templates/templates.go`](../templates/templates.go) for the loader):

- `template.yaml` -- manifest: name, description, `whenToUse`,
  parameters, applicability.
- `pipeline.go.tmpl` -- the Go body, a `text/template` with
  `{{.param}}` substitution and `{{if .param}}...{{end}}` for elidable
  steps.
- `README.md` -- prose: when to use, when not to, the parameter table,
  what to edit after rendering.

The manifest's `whenToUse` is the catalog signal: it answers "which
template do I pick?" not just "what exists?". Write it for an agent
choosing among starters.

## 3. Pipeline primitives (the conveniences)

A primitive (`pipelines.DockerDeploy`) is a block-shaped *whole
pipeline*: a struct a consumer embeds and configures, upgraded via
`go get`. Primitives stay thin -- a composition of blocks with minimal
branching -- so they read like a template that happens to be compiled
in. That thinness is also what would let a primitive be re-expressed as
a template later (and a template render produce the primitive's library
form); don't write monolithic, deeply-branched primitives that foreclose
it.

Prefer a template over a new primitive unless consumers specifically
want the import-and-configure, upgrade-via-`go get` experience for a
pattern that is genuinely stable.

## Adding to the registry

When you add a module, add it to [`spark.json`](../spark.json) and to
[`go.work`](../go.work) (a `use` line and a `replace` to the local
path). When you add a template, drop the directory, append its name to
`templateNames` in `templates/templates.go`, add it to the `go:embed`
line, and add a render case to `templates_test.go`. Every template must
render to parseable Go and carry a non-empty README and `whenToUse`.

## Rendering a template

The normal path is `sparkwing pipeline new --template <name> --param
k=v`. To render by hand (or from a tool), call `templates.Render(name,
map[string]string{...})`. Two non-obvious rules:

- **Param names are hyphenated in the manifest and CLI (`health-url`) but
  underscored inside the body (`{{.health_url}}`).** The renderer
  translates; you pass them hyphenated.
- **Passing a param as explicit-empty (`--param test-cmd=`) is honored as
  "no value" and elides any `{{if .test_cmd}}` step** -- it does NOT fall
  back to the manifest default. Omit the flag to get the default.

## Consuming sparks-core before a release (local dev)

A `.sparkwing/` that depends on an unreleased change here needs its
module graph pointed at the working tree. Two things bite:

- **Replace the whole dependency subtree, not just your direct imports.**
  An unpublished module needs a `replace` (or `go.work use`) for every
  sparks-core module in its transitive graph. Importing `docker` also
  pulls `step` and `aws`, so all three need a local override even though
  you only `import` one. (The `require` version paired with a `replace`
  is irrelevant -- `replace` wins -- so any plausible version is fine.)
- **`GOWORK=off` when the checkout sits under another workspace.** If
  `sparkwing` / `sparks-core` live under a directory that has its own
  `go.work`, a stray parent workspace will shadow your resolution and you
  get confusing "main module does not contain package" errors. Build the
  consumer `.sparkwing/` with `GOWORK=off` (or its own `go.work`).
