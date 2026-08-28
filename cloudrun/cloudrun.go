// Package cloudrun deploys container images or source trees to Google
// Cloud Run behind the gcloud CLI, discovers service URLs, shifts traffic
// between revisions, and rolls back. Mutating operations honor
// SPARKWING_DRY_RUN (or a call's DryRun field) by echoing the gcloud argv;
// state reads always execute.
package cloudrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gcp"
	"github.com/sparkwing-dev/sparks-core/step"
)

// DeployConfig drives Deploy and DeploySource. Every field maps to the
// like-named `gcloud run deploy` flag, and an empty or zero value omits
// that flag so Cloud Run's own default applies.
type DeployConfig struct {
	Service string
	// Image is deployed only when Source is empty.
	Image string
	// Source switches to a `--source` buildpacks deploy.
	Source string
	Region string
	// Project empty falls back to the ambient gcloud project.
	Project                   string
	ImpersonateServiceAccount string
	Port                      int
	Env                       map[string]string
	// AllowUnauthenticated always emits a flag: --allow-unauthenticated
	// when true, --no-allow-unauthenticated when false.
	AllowUnauthenticated bool
	NoTraffic            bool
	// Tag yields a stable per-tag preview URL; with NoTraffic it never
	// serves production traffic.
	Tag            string
	Memory         string
	CPU            string
	MinInstances   int
	MaxInstances   int
	Concurrency    int
	Timeout        string
	ServiceAccount string
	// ExtraArgs reach gcloud flags this struct does not model. Runtime
	// secret values belong in sparkwing secrets, not here.
	ExtraArgs []string
	// DryRun forces echo-and-skip even when SPARKWING_DRY_RUN is unset.
	DryRun bool
}

type Ref struct {
	Service                   string
	Region                    string
	Project                   string
	ImpersonateServiceAccount string
}

// DeployResult carries the URL to probe plus the revision handles a
// targeted rollback needs. Every field is empty under dry-run.
type DeployResult struct {
	// URL is the tag's preview URL for a tagged deploy, else the service URL.
	URL      string
	Revision string
	// PriorRevision was serving before this deploy; pass it to
	// RollbackToRevision. Empty on a first deploy.
	PriorRevision string
}

// Deploy rolls Image (or Source, when set) out to the Cloud Run service.
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
		prior, perr := currentReadyRevision(ctx, ref)
		if perr != nil {
			sparkwing.Info(ctx, "cloudrun: could not read prior revision for %q; precise rollback handle unset: %v", cfg.Service, perr)
		}
		out, err := sparkwing.Exec(ctx, "gcloud", args...).String()
		if err != nil {
			return fmt.Errorf("cloudrun: deploy %q: %w", cfg.Service, err)
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

// DeploySource is Deploy with cfg.Source defaulted to the current directory.
func DeploySource(ctx context.Context, cfg DeployConfig) (*DeployResult, error) {
	if cfg.Source == "" {
		cfg.Source = "."
	}
	return Deploy(ctx, cfg)
}

// ServiceURL returns the main URL of a Cloud Run service.
func ServiceURL(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", describeArgs(ref)...).String()
	if err != nil {
		return "", fmt.Errorf("cloudrun: describe %q: %w", ref.Service, err)
	}
	return parseServiceURL([]byte(out)), nil
}

// currentReadyRevision returns empty with no error when the service does not
// yet exist; every other read failure is returned so a missing rollback
// handle is never swallowed.
func currentReadyRevision(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", describeArgs(ref)...).String()
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("cloudrun: describe %q: %w", ref.Service, err)
	}
	return parseLatestReadyRevision([]byte(out)), nil
}

// isNotFound separates a first-ever deploy from a transient auth or network
// failure by matching gcloud's stderr.
func isNotFound(err error) bool {
	var ee *sparkwing.ExecError
	if !errors.As(err, &ee) {
		return false
	}
	s := strings.ToLower(ee.Stderr)
	return strings.Contains(s, "not_found") ||
		strings.Contains(s, "cannot find") ||
		strings.Contains(s, "could not find") ||
		strings.Contains(s, "does not exist")
}

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
	args = append(args, gcp.ImpersonationArgs(cfg.ImpersonateServiceAccount)...)
	if cfg.Port > 0 {
		args = append(args, "--port", strconv.Itoa(cfg.Port))
	}
	if len(cfg.Env) > 0 {
		args = append(args, "--set-env-vars", joinEnv(cfg.Env))
	}
	if cfg.Memory != "" {
		args = append(args, "--memory", cfg.Memory)
	}
	if cfg.CPU != "" {
		args = append(args, "--cpu", cfg.CPU)
	}
	if cfg.MinInstances > 0 {
		args = append(args, "--min-instances", strconv.Itoa(cfg.MinInstances))
	}
	if cfg.MaxInstances > 0 {
		args = append(args, "--max-instances", strconv.Itoa(cfg.MaxInstances))
	}
	if cfg.Concurrency > 0 {
		args = append(args, "--concurrency", strconv.Itoa(cfg.Concurrency))
	}
	if cfg.Timeout != "" {
		args = append(args, "--timeout", cfg.Timeout)
	}
	if cfg.ServiceAccount != "" {
		args = append(args, "--service-account", cfg.ServiceAccount)
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
	args = append(args, cfg.ExtraArgs...)
	return append(args, "--quiet", "--format=json")
}

func describeArgs(ref Ref) []string {
	args := []string{"run", "services", "describe", ref.Service}
	if ref.Region != "" {
		args = append(args, "--region", ref.Region)
	}
	args = append(args, gcp.ProjectArgs(ref.Project)...)
	args = append(args, gcp.ImpersonationArgs(ref.ImpersonateServiceAccount)...)
	return append(args, "--format=json")
}

// joinEnv sorts keys for a deterministic command line, and switches to
// gcloud's ^delim^ escape syntax when a key or value contains the default
// comma delimiter.
func joinEnv(env map[string]string) string {
	keys := make([]string, 0, len(env))
	hasComma := false
	for k, v := range env {
		keys = append(keys, k)
		if strings.ContainsRune(k, ',') || strings.ContainsRune(v, ',') {
			hasComma = true
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	if !hasComma {
		return strings.Join(pairs, ",")
	}
	delim := pickEnvDelimiter(env)
	return "^" + delim + "^" + strings.Join(pairs, delim)
}

// pickEnvDelimiter returns a character absent from every key and value, or
// "@" when all candidates are present.
func pickEnvDelimiter(env map[string]string) string {
	for _, c := range []string{"@", "#", "|", ";", "~", "!", "%", "+"} {
		if !envContains(env, c) {
			return c
		}
	}
	return "@"
}

func envContains(env map[string]string, s string) bool {
	for k, v := range env {
		if strings.Contains(k, s) || strings.Contains(v, s) {
			return true
		}
	}
	return false
}

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

func parseServiceURL(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.URL
}

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

func parseLatestReadyRevision(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.LatestReadyRevisionName
}

func parseLatestCreatedRevision(data []byte) string {
	var s serviceDescribe
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.Status.LatestCreatedRevisionName
}

func isDryRun(force bool) bool {
	return force || os.Getenv("SPARKWING_DRY_RUN") != ""
}

func echoArgv(ctx context.Context, name string, args []string) {
	sparkwing.Info(ctx, "DRY RUN: %s %s", name, strings.Join(args, " "))
}
