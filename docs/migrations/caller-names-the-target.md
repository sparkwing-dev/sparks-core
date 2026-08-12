# The caller names the target

Modules: `aws`, `s3`, `pipelines`, `gcp`, `cloudrun`, `kube`, `docker`,
`deploy`, `rollback`.

`ProfileArgs` and `ProfileFlag` used to read `AWS_PROFILE` and pass it as
`--profile`, overriding whatever the caller asked for. They now pass the
caller's profile and nothing else.

Passing `--profile` does not select credentials from the aws CLI's
chain. It overrides the chain, the same way `Session(profile_name=...)`
suppresses botocore's environment provider. Doing that from an inherited
variable can redirect a deploy to another account without a line of the
pipeline changing, which is the failure this removes.

## What changes

| Caller | Before | After |
|---|---|---|
| Names a profile | `AWS_PROFILE` overrode it | the named profile is used |
| Names nothing, `AWS_PROFILE` set | `--profile $AWS_PROFILE` | no `--profile`, and the aws CLI reads `AWS_PROFILE` itself |
| Names nothing, environment credentials set | `--profile` from the environment, if any | no `--profile`, and the credentials are used |
| Runs under IRSA | no `--profile` | unchanged |

The middle row is the one to check. The CLI still honors `AWS_PROFILE`,
so the same profile applies unless environment credentials outrank it,
which is the precedence boto3 uses.

## Before

```go
// Empty AWSProfile was an error, so CI had to invent a profile that
// named credentials it already had.
sd := pipelines.StaticDeploy{
    Bucket:     "example-site",
    AWSProfile: os.Getenv("AWS_PROFILE"), // or a hardcoded fallback
}
```

## After

```go
// A laptop pins the profile it means.
sd := pipelines.StaticDeploy{
    Bucket:            "example-site",
    AWSProfile:        "my-profile",
    ExpectedAccountID: "123456789012",
}

// CI running as an assumed role names no profile, because there is
// none to name, and states the account instead.
sd := pipelines.StaticDeploy{
    Bucket:            "example-site",
    ExpectedAccountID: "123456789012",
}
```

## Upgrade steps

1. Pass the profile you mean as an argument. Remove any `os.Getenv("AWS_PROFILE")`
   read that fed it, because the CLI reads that variable itself.
2. Set `ExpectedAccountID` on anything that deletes or overwrites. It runs
   `sts get-caller-identity` before the first write and fails on a
   mismatch. A profile name pins which credentials get selected and not
   which account they belong to, and under federated auth there is no
   profile to name at all.
3. Drop the `StaticDeploy` workaround if you have one. An empty
   `AWSProfile` used to fail with "AWSProfile is required", and callers
   worked around it by writing a credentials file naming the session
   they already had. Empty is now the supported way to say "use the
   environment's credentials".
4. Check any doc comment in your own pipelines that promises an
   `AWS_PROFILE` fallback.

## GCP

`gcp` had the same shape with different variables. `ProjectArgs` read
`GOOGLE_CLOUD_PROJECT` and `CLOUDSDK_CORE_PROJECT`; `ImpersonationArgs`
took no argument at all and read
`CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT`, so an inherited variable
decided which identity every gcloud call ran as.

Both now take what the caller passes. gcloud reads those variables
itself when no flag is given, so the same project and identity still
apply, with gcloud's precedence rather than an override.

### Before

```go
// Identity came from the environment, invisible to the pipeline.
args = append(args, gcp.ProjectArgs(cfg.Project)...)
args = append(args, gcp.ImpersonationArgs()...)
```

### After

```go
args = append(args, gcp.ProjectArgs(cfg.Project)...)
args = append(args, gcp.ImpersonationArgs(cfg.ImpersonateServiceAccount)...)
```

`ResolveProject` is gone. It existed to hold the environment fallback,
so pass the project to `ProjectArgs` directly.

Cloud Run callers set `ImpersonateServiceAccount` on `DeployConfig`,
`Ref`, `TrafficConfig`, or `RollbackConfig`. Leaving it empty keeps
gcloud's own handling of the variable, so a caller that relied on the
environment keeps working without editing anything.

## Kubernetes and kind

`kube.ResolveContext` already failed closed, so it could not silently
pick a cluster. It could still pick the wrong one: `SPARKWING_KUBE_CONTEXT`
named a context, `SPARKWING_KIND_CLUSTER` derived one, and
`SPARKWING_KUBE_ALLOW_CURRENT=1` disabled the guard from the
environment. All three are gone. Set `Context` on the config you pass.
In-cluster runs are unaffected, because a service account is a fact
about the machine rather than a choice.

Kind support is removed rather than made explicit, because nothing
deploys to kind any more:

| Module | Removed |
|---|---|
| `docker` | the `kind load docker-image` path in `BuildAndPush` |
| `kube` | `DeployKindKustomize`, `KindKustomizeConfig`, `DeployKustomize`, `DeployKustomizeConfig` |
| `deploy` | the branch that turned a `Local: false` deploy into a kind deploy |
| `rollback` | the same override on its routing condition |

`deploy` and `rollback` route on `cfg.Local` alone now. A pipeline that
built for a kind cluster needs a registry the cluster can pull from, or
a different deploy path entirely.

## Not covered by a snapshot

The `gcp` break is structural and a compiler catches it:
`ImpersonationArgs` gained a parameter and `ResolveProject` is gone.

The `aws` break is behavioral. Signatures are unchanged, every consumer
compiles, and the tests pass, so nothing tells you it happened. `ecs`,
`lambda`, `dbbackup`, and `docker` were each built and tested against
the new `aws` module with a local replace, and all four are green. What
changes for them is which profile reaches the CLI once their `aws` pin
moves, and their doc comments still promise the old fallback:

- `lambda/lambda.go:65`, `:97`, `:121`
- `ecs/ecs.go:93`
- `docker/login.go:70`

Those comments need updating in the same wave that bumps their pin.
