package cloudrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/gcp"
	"github.com/sparkwing-dev/sparks-core/step"
)

// TrafficConfig drives Traffic. Set ToLatest to route all traffic to
// the latest ready revision, or Revision (with an optional Percent,
// defaulting to 100) to route to a named revision.
type TrafficConfig struct {
	Service  string
	Region   string
	Project  string
	Revision string
	Percent  int
	ToLatest bool
	DryRun   bool
}

// RollbackConfig drives RollbackToRevision. Revision is the target to
// shift traffic back to; leave it empty to let the rollback discover the
// prior ready revision at run time (the shape a Plan-time OnFailure hook
// needs, since it cannot see Deploy's returned handles).
type RollbackConfig struct {
	Service  string
	Region   string
	Project  string
	Revision string
	DryRun   bool
}

// Traffic returns a func(ctx) error that shifts Cloud Run traffic per
// cfg, shaped to plug into a sparkwing Job body or hook. Honors
// SPARKWING_DRY_RUN / cfg.DryRun.
func Traffic(cfg TrafficConfig) func(context.Context) error {
	return func(ctx context.Context) error {
		return step.Run(ctx, "cloud run shift traffic ("+cfg.Service+")", func(ctx context.Context) error {
			args := trafficArgs(cfg)
			if isDryRun(cfg.DryRun) {
				echoArgv(ctx, "gcloud", args)
				return nil
			}
			return step.Exec(ctx, "gcloud", args...)
		})
	}
}

// RollbackToRevision returns a func(ctx) error that shifts all traffic
// back to a prior revision, shaped for a Job's OnFailure hook. When
// cfg.Revision is set it targets that revision; when empty it discovers
// the previous ready revision at run time. Honors SPARKWING_DRY_RUN /
// cfg.DryRun.
func RollbackToRevision(cfg RollbackConfig) func(context.Context) error {
	return func(ctx context.Context) error {
		return step.Run(ctx, "cloud run rollback ("+cfg.Service+")", func(ctx context.Context) error {
			if isDryRun(cfg.DryRun) {
				target := cfg.Revision
				if target == "" {
					target = "PRIOR_REVISION"
				}
				echoArgv(ctx, "gcloud", trafficArgs(revisionTraffic(cfg, target)))
				return nil
			}
			rev := cfg.Revision
			if rev == "" {
				discovered, err := priorReadyRevision(ctx, Ref{Service: cfg.Service, Region: cfg.Region, Project: cfg.Project})
				if err != nil {
					return err
				}
				rev = discovered
			}
			if rev == "" {
				return fmt.Errorf("cloudrun: no prior revision to roll back service %q to", cfg.Service)
			}
			return step.Exec(ctx, "gcloud", trafficArgs(revisionTraffic(cfg, rev))...)
		})
	}
}

// Rollback is an alias for RollbackToRevision: a func(ctx) error that
// rolls a service's traffic back to a prior revision, honoring
// SPARKWING_DRY_RUN.
func Rollback(cfg RollbackConfig) func(context.Context) error {
	return RollbackToRevision(cfg)
}

// RemoveTag removes a revision tag from a service (preview teardown) via
// update-traffic --remove-tags. Honors SPARKWING_DRY_RUN / cfg.DryRun.
func RemoveTag(ctx context.Context, cfg DeployConfig) error {
	return step.Run(ctx, "cloud run remove tag ("+cfg.Tag+")", func(ctx context.Context) error {
		args := removeTagArgs(cfg)
		if isDryRun(cfg.DryRun) {
			echoArgv(ctx, "gcloud", args)
			return nil
		}
		return step.Exec(ctx, "gcloud", args...)
	})
}

// revisionTraffic is the TrafficConfig that routes 100% to rev, reusing
// the service coordinates from a RollbackConfig.
func revisionTraffic(cfg RollbackConfig, rev string) TrafficConfig {
	return TrafficConfig{Service: cfg.Service, Region: cfg.Region, Project: cfg.Project, Revision: rev}
}

// trafficArgs builds the `gcloud run services update-traffic ...` argv.
func trafficArgs(cfg TrafficConfig) []string {
	args := []string{"run", "services", "update-traffic", cfg.Service}
	if cfg.Region != "" {
		args = append(args, "--region", cfg.Region)
	}
	args = append(args, gcp.ProjectArgs(cfg.Project)...)
	args = append(args, gcp.ImpersonationArgs()...)
	switch {
	case cfg.ToLatest:
		args = append(args, "--to-latest")
	case cfg.Revision != "":
		pct := cfg.Percent
		if pct == 0 {
			pct = 100
		}
		args = append(args, "--to-revisions", cfg.Revision+"="+strconv.Itoa(pct))
	}
	return append(args, "--quiet")
}

// removeTagArgs builds the `gcloud run services update-traffic
// --remove-tags ...` argv for a preview teardown.
func removeTagArgs(cfg DeployConfig) []string {
	args := []string{"run", "services", "update-traffic", cfg.Service}
	if cfg.Region != "" {
		args = append(args, "--region", cfg.Region)
	}
	args = append(args, gcp.ProjectArgs(cfg.Project)...)
	args = append(args, gcp.ImpersonationArgs()...)
	args = append(args, "--remove-tags", cfg.Tag)
	return append(args, "--quiet")
}

// revisionsListArgs builds the `gcloud run revisions list ...` argv,
// newest first, for prior-revision discovery.
func revisionsListArgs(ref Ref) []string {
	args := []string{"run", "revisions", "list", "--service", ref.Service}
	if ref.Region != "" {
		args = append(args, "--region", ref.Region)
	}
	args = append(args, gcp.ProjectArgs(ref.Project)...)
	args = append(args, gcp.ImpersonationArgs()...)
	return append(args, "--format=json", "--sort-by=~metadata.creationTimestamp")
}

// priorReadyRevision returns the second-newest revision of a service:
// after a fresh (failed) deploy the newest revision is the one being
// rolled away from, so the one before it is the rollback target.
func priorReadyRevision(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", revisionsListArgs(ref)...).String()
	if err != nil {
		return "", err
	}
	names := parseRevisionNames([]byte(out))
	if len(names) < 2 {
		return "", nil
	}
	return names[1], nil
}

// parseRevisionNames extracts metadata.name from a `gcloud run revisions
// list --format=json` document, preserving the listed order.
func parseRevisionNames(data []byte) []string {
	var revs []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if json.Unmarshal(data, &revs) != nil {
		return nil
	}
	names := make([]string, 0, len(revs))
	for _, r := range revs {
		names = append(names, r.Metadata.Name)
	}
	return names
}
