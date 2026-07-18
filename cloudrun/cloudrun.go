// Package cloudrun orchestrates Google Cloud Run deploys behind the
// gcloud CLI: roll a container image (or a source tree) out to a
// service, discover the service URL, shift traffic between revisions,
// and roll back to a prior revision on a failed verify.
//
// It sits above the [github.com/sparkwing-dev/sparks-core/gcp] module,
// reusing gcp.ProjectArgs / gcp.ImpersonationArgs so every gcloud
// invocation carries the same project and impersonation flags the rest
// of a GCP pipeline uses. Deploy is the image-or-source entry point;
// DeploySource is the source-only convenience; Traffic and
// RollbackToRevision return func(ctx) error closures shaped to drop
// straight into a sparkwing Job.Verify / OnFailure hook.
//
// Cloud-mutating operations (Deploy, DeploySource, Traffic,
// RollbackToRevision, RemoveTag) honor SPARKWING_DRY_RUN: when it is
// non-empty (or the call's DryRun field is set) they echo the exact
// gcloud argv they would run and return success without executing, so a
// scaffolded pipeline goes green locally with no GCP credentials.
// State-reading helpers (ServiceURL and the internal revision lookups)
// execute for real, since there is nothing to mutate.
//
// The gcloud CLI must be on PATH.
package cloudrun

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gcp"
	"github.com/sparkwing-dev/sparks-core/step"
)

// DeployConfig drives Deploy and DeploySource. A zero Port omits the
// --port flag (Cloud Run's own default applies); an empty Region lets
// gcloud resolve the region from its config. Set Source for a
// source-based (Cloud Build + buildpacks) deploy; when Source is empty
// Image is deployed instead.
type DeployConfig struct {
	// Service is the Cloud Run service name to create or update.
	Service string
	// Image is the fully-qualified container image to deploy. Ignored
	// when Source is set.
	Image string
	// Source is a source directory. When non-empty the deploy uses
	// `gcloud run deploy --source <dir>` (server-side buildpacks) and
	// Image is ignored.
	Source string
	// Region is the Cloud Run region (e.g. "us-west1").
	Region string
	// Project is the GCP project id; empty falls back to the ambient
	// gcloud project (see gcp.ProjectArgs).
	Project string
	// Port is the container port the service listens on. Zero omits
	// --port and Cloud Run applies its default.
	Port int
	// Env is the runtime environment passed via --set-env-vars. Keys are
	// emitted in sorted order for a stable command line.
	Env map[string]string
	// AllowUnauthenticated selects --allow-unauthenticated (public) when
	// true, or --no-allow-unauthenticated (private) when false.
	AllowUnauthenticated bool
	// NoTraffic deploys the new revision without shifting traffic to it
	// (--no-traffic). Combine with Tag for a preview revision.
	NoTraffic bool
	// Tag assigns a revision tag (--tag), yielding a stable per-tag
	// preview URL. With NoTraffic it produces a preview that never
	// serves production traffic.
	Tag string
	// DryRun forces the echo-and-skip behavior for this call even when
	// SPARKWING_DRY_RUN is unset.
	DryRun bool
}

// Ref identifies a Cloud Run service for a state-reading lookup.
type Ref struct {
	Service string
	Region  string
	Project string
}

// DeployResult is what Deploy returns: the URL to probe plus the
// revision handles that make a targeted rollback possible.
type DeployResult struct {
	// URL is the service URL to probe. For a tagged preview deploy it is
	// the tag's preview URL; otherwise the service's main URL. Empty
	// under dry-run.
	URL string
	// Revision is the revision this deploy created (best-effort; empty
	// under dry-run or when gcloud reports no name).
	Revision string
	// PriorRevision is the revision that was serving before this deploy.
	// Pass it to RollbackToRevision for a precise rollback. Empty when
	// this is the service's first deploy or under dry-run.
	PriorRevision string
}

// Deploy rolls Image (or Source, when set) out to the Cloud Run service
// and returns the URL to probe together with the revision that was
// serving beforehand, so a failed verify can roll back to it precisely.
//
// Under SPARKWING_DRY_RUN (or cfg.DryRun) it echoes the gcloud argv and
// returns an empty DeployResult without touching the service.
func Deploy(ctx context.Context, cfg DeployConfig) (*DeployResult, error) {
	var res *DeployResult
	err := step.Run(ctx, "cloud run deploy ("+cfg.Service+")", func(ctx context.Context) error {
		args := deployArgs(cfg)
		if isDryRun(cfg.DryRun) {
			echoArgv(ctx, "gcloud", args)
			res = &DeployResult{}
			return nil
		}
		ref := Ref{Service: cfg.Service, Region: cfg.Region, Project: cfg.Project}
		prior, _ := currentReadyRevision(ctx, ref)
		out, err := sparkwing.Exec(ctx, "gcloud", args...).String()
		if err != nil {
			return err
		}
		r := &DeployResult{PriorRevision: prior, Revision: parseLatestCreatedRevision([]byte(out))}
		if cfg.Tag != "" {
			r.URL = parseTaggedURL([]byte(out), cfg.Tag)
		} else {
			r.URL = parseServiceURL([]byte(out))
		}
		if r.URL == "" {
			if u, e := ServiceURL(ctx, ref); e == nil {
				r.URL = u
			}
		}
		res = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DeploySource is the source-based (Dockerfile-free) deploy: it forces a
// `gcloud run deploy --source` build via Cloud Build buildpacks. An
// empty cfg.Source defaults to the current directory.
func DeploySource(ctx context.Context, cfg DeployConfig) (*DeployResult, error) {
	if cfg.Source == "" {
		cfg.Source = "."
	}
	return Deploy(ctx, cfg)
}

// ServiceURL returns the main URL of a Cloud Run service by describing
// it. It is a state read and always executes gcloud (there is nothing
// to mutate), so unlike Deploy it does not honor SPARKWING_DRY_RUN.
func ServiceURL(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", describeArgs(ref)...).String()
	if err != nil {
		return "", err
	}
	return parseServiceURL([]byte(out)), nil
}

// currentReadyRevision returns the service's latest ready revision, the
// one serving before a deploy. Empty (no error) when the service does
// not yet exist.
func currentReadyRevision(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", describeArgs(ref)...).String()
	if err != nil {
		return "", nil
	}
	return parseLatestReadyRevision([]byte(out)), nil
}

// deployArgs builds the `gcloud run deploy ...` argv (without the
// leading "gcloud"), folding in the resolved project and any
// impersonation target.
func deployArgs(cfg DeployConfig) []string {
	args := []string{"run", "deploy", cfg.Service}
	if cfg.Source != "" {
		args = append(args, "--source", cfg.Source)
	} else {
		args = append(args, "--image", cfg.Image)
	}
	if cfg.Region != "" {
		args = append(args, "--region", cfg.Region)
	}
	args = append(args, gcp.ProjectArgs(cfg.Project)...)
	args = append(args, gcp.ImpersonationArgs()...)
	if cfg.Port > 0 {
		args = append(args, "--port", strconv.Itoa(cfg.Port))
	}
	if len(cfg.Env) > 0 {
		args = append(args, "--set-env-vars", joinEnv(cfg.Env))
	}
	if cfg.AllowUnauthenticated {
		args = append(args, "--allow-unauthenticated")
	} else {
		args = append(args, "--no-allow-unauthenticated")
	}
	if cfg.NoTraffic {
		args = append(args, "--no-traffic")
	}
	if cfg.Tag != "" {
		args = append(args, "--tag", cfg.Tag)
	}
	return append(args, "--quiet", "--format=json")
}

// describeArgs builds the `gcloud run services describe ...` argv for a
// state read.
func describeArgs(ref Ref) []string {
	args := []string{"run", "services", "describe", ref.Service}
	if ref.Region != "" {
		args = append(args, "--region", ref.Region)
	}
	args = append(args, gcp.ProjectArgs(ref.Project)...)
	args = append(args, gcp.ImpersonationArgs()...)
	return append(args, "--format=json")
}

// joinEnv renders an env map as "k=v,k=v" with keys sorted, so the
// emitted --set-env-vars value is deterministic.
func joinEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	return strings.Join(pairs, ",")
}

// serviceDescribe is the slice of a Cloud Run service resource this
// package reads out of `gcloud ... --format=json`.
type serviceDescribe struct {
	Status struct {
		URL                       string `json:"url"`
		LatestReadyRevisionName   string `json:"latestReadyRevisionName"`
		LatestCreatedRevisionName string `json:"latestCreatedRevisionName"`
		Traffic                   []struct {
			RevisionName string `json:"revisionName"`
			Tag          string `json:"tag"`
			URL          string `json:"url"`
			Percent      int    `json:"percent"`
		} `json:"traffic"`
	} `json:"status"`
}

// parseServiceURL extracts status.url from a service-describe JSON
// document, or "" when absent/unparseable.
func parseServiceURL(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.URL
}

// parseTaggedURL returns the preview URL of the traffic target carrying
// tag, or "" when no such target exists.
func parseTaggedURL(data []byte, tag string) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	for _, t := range s.Status.Traffic {
		if t.Tag == tag {
			return t.URL
		}
	}
	return ""
}

// parseLatestReadyRevision extracts status.latestReadyRevisionName.
func parseLatestReadyRevision(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.LatestReadyRevisionName
}

// parseLatestCreatedRevision extracts status.latestCreatedRevisionName.
func parseLatestCreatedRevision(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.LatestCreatedRevisionName
}

// isDryRun reports whether this call should echo-and-skip: either its
// own DryRun override is set, or SPARKWING_DRY_RUN is non-empty.
func isDryRun(force bool) bool {
	return force || os.Getenv("SPARKWING_DRY_RUN") != ""
}

// echoArgv logs the exact command a cloud-mutating step would run under
// dry-run, mirroring the gcp module's convention.
func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}
