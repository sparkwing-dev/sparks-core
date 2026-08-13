# The caller names the registry and the ArgoCD server

Modules: `docker`, `gitops`, `deploy`, `rollback`, `pipelines`.

`docker` read `SPARKWING_REGISTRY` and `SPARKWING_ECR_REGISTRY` to
decide where an image was pushed. `gitops` read
`SPARKWING_ARGOCD_SERVER` and `SPARKWING_ARGOCD_TOKEN` to decide which
ArgoCD it synced. Both now take what the caller passes, which finishes
the sweep [the caller names the target](caller-names-the-target.md)
started: every module in this repo takes its target as an argument.

The ArgoCD token is the clearer of the two. It is a credential, and
there was no field to pass it in, so a caller that resolved it properly
had to put it back where the library would look:

```go
token, err := sparkwing.Secret(ctx, "SPARKWING_ARGOCD_TOKEN")
if err != nil {
    return err
}
_ = os.Setenv("SPARKWING_ARGOCD_TOKEN", token) // the workaround
```

A workaround that writes a secret into the process environment is the
evidence the API was wrong.

## docker

| Before | After |
|---|---|
| `DetectRegistries(cluster, defaultECR)` | `Registries(registry, ecrRegistry)` |
| `DetectLocalRegistries(cluster)` | `RequireLocalRegistry(registry)` |
| `TryDetectLocalRegistries(cluster)` | `LocalRegistries(registry)` — returns `[]string`, no error |

The names lose "Detect" because nothing is detected once the caller
names the registry. The `cluster` parameter is gone: no function body
had read it since kind support was removed in v0.26.0, so it described
a selection that no longer happened.

### Before

```go
// SPARKWING_REGISTRY, wherever it came from, chose the push target.
registries, err := docker.DetectRegistries("sparktest", defaultECR)
```

### After

```go
// The pipeline states its target. Resolve a runner override through a
// pipeline argument or a secret, not an inherited variable.
registries, err := docker.Registries(r.Registry, r.ECR)
```

`Registries` returns the named registry when there is one and
`ecrRegistry` otherwise, and errors when both are empty. That is the old
precedence with the environment taken out of it.

## gitops, deploy, rollback

`SyncArgoCD` gained an `ArgoCDConfig` parameter:

```go
func SyncArgoCD(ctx context.Context, argocd ArgoCDConfig, appName string, tag ...string) error
```

`deploy.Config` and `rollback.Config` carry the same struct as an
`ArgoCD` field and pass it through.

### Before

```go
token, err := sparkwing.Secret(ctx, "SPARKWING_ARGOCD_TOKEN")
if err != nil {
    return err
}
_ = os.Setenv("SPARKWING_ARGOCD_TOKEN", token)
return deploy.Run(ctx, deploy.Config{AppName: "myapp" /* ... */})
```

### After

```go
token, err := sparkwing.Secret(ctx, "ARGOCD_TOKEN")
if err != nil {
    return err
}
return deploy.Run(ctx, deploy.Config{
    AppName: "myapp",
    ArgoCD: gitops.ArgoCDConfig{
        Server: "https://argocd.example.com",
        Token:  token,
    },
    // ...
})
```

The in-cluster probe is unchanged. An empty `Server` still tries
`http://argocd-server.argocd.svc.cluster.local:80` and still fails
closed when it is unreachable, because where the code runs is a fact
about the machine rather than a choice of target.

## pipelines

`DockerDeploy` gains `Registry` and `ArgoCD`, and loses `Cluster`:

```go
&pipelines.DockerDeploy{
    Image:    "myapp",
    ECR:      "123456789012.dkr.ecr.us-west-2.amazonaws.com",
    Registry: "",                            // empty pushes to ECR
    ArgoCD:   gitops.ArgoCDConfig{Token: t}, // empty Server probes in-cluster
}
```

`Cluster` defaulted to `"sparktest"` and fed a registry lookup that
ignored it, so deleting it changes no behavior.

## Still read from the environment

Two families survive, and both are declared in the README:

- `SPARKWING_DRY_RUN`, because it can only make a run less destructive.
- The harness channel: `SPARKWING_JOB_ID`, `SPARKWING_PIPELINE`,
  `SPARKWING_COMMIT`, `SPARKWING_CONTROLLER`, `SPARKWING_API_TOKEN`, and
  the `SPARKWING_NO_VERIFY` break-glass flag. The runner sets these on a
  job it dispatched. They describe the run, not the target.

## Upgrade steps

1. Pass the registry you mean. Any `os.Getenv("SPARKWING_REGISTRY")` in
   your own pipeline goes too: nothing downstream reads it any more, so
   leaving the read in place silently does nothing.
2. Set `ArgoCD` on the `deploy.Config` or `rollback.Config` you build,
   resolving the token through `sparkwing.Secret`.
3. Delete any `os.Setenv("SPARKWING_ARGOCD_SERVER")` or
   `os.Setenv("SPARKWING_ARGOCD_TOKEN")` workaround.
4. Drop `DockerDeploy.Cluster`; the compiler will point at it.
5. Rename the `docker` calls per the table above.
