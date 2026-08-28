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

// TrafficConfig drives Traffic. ToLatest routes to the latest ready
// revision; Revision routes to a named one at Percent, defaulting to 100.
type TrafficConfig struct {
	Service                   string
	Region                    string
	Project                   string
	Revision                  string
	Percent                   int
	ToLatest                  bool
	DryRun                    bool
	ImpersonateServiceAccount string
}

// RollbackConfig drives RollbackToRevision. Prefer an explicit Revision,
// which Deploy captures as DeployResult.PriorRevision: empty-Revision
// discovery falls back to the newest Ready revision below the latest, which
// a Ready no-traffic preview revision can fool.
type RollbackConfig struct {
	Service                   string
	Region                    string
	Project                   string
	Revision                  string
	DryRun                    bool
	ImpersonateServiceAccount string
}

// Traffic returns a closure that shifts Cloud Run traffic per cfg.
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

// RollbackToRevision returns an OnFailure-shaped closure that shifts all
// traffic to cfg.Revision, or to a revision discovered at run time when it
// is empty.
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

// Rollback is an alias for RollbackToRevision.
func Rollback(cfg RollbackConfig) func(context.Context) error {
	return RollbackToRevision(cfg)
}

// RemoveTag removes a revision tag from a service, tearing down its preview.
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

func revisionTraffic(cfg RollbackConfig, rev string) TrafficConfig {
	return TrafficConfig{Service: cfg.Service, Region: cfg.Region, Project: cfg.Project, Revision: rev}
}

func trafficArgs(cfg TrafficConfig) []string {
	args := []string{"run", "services", "update-traffic", cfg.Service}
	if cfg.Region != "" {
		args = append(args, "--region", cfg.Region)
	}
	args = append(args, gcp.ProjectArgs(cfg.Project)...)
	args = append(args, gcp.ImpersonationArgs(cfg.ImpersonateServiceAccount)...)
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

func removeTagArgs(cfg DeployConfig) []string {
	args := []string{"run", "services", "update-traffic", cfg.Service}
	if cfg.Region != "" {
		args = append(args, "--region", cfg.Region)
	}
	args = append(args, gcp.ProjectArgs(cfg.Project)...)
	args = append(args, gcp.ImpersonationArgs(cfg.ImpersonateServiceAccount)...)
	args = append(args, "--remove-tags", cfg.Tag)
	return append(args, "--quiet")
}

func revisionsListArgs(ref Ref) []string {
	args := []string{"run", "revisions", "list", "--service", ref.Service}
	if ref.Region != "" {
		args = append(args, "--region", ref.Region)
	}
	args = append(args, gcp.ProjectArgs(ref.Project)...)
	args = append(args, gcp.ImpersonationArgs(ref.ImpersonateServiceAccount)...)
	return append(args, "--format=json", "--sort-by=~metadata.creationTimestamp")
}

func priorReadyRevision(ctx context.Context, ref Ref) (string, error) {
	out, err := sparkwing.Exec(ctx, "gcloud", revisionsListArgs(ref)...).String()
	if err != nil {
		return "", fmt.Errorf("cloudrun: list revisions for %q: %w", ref.Service, err)
	}
	return priorReadyRevisionName(parseRevisions([]byte(out))), nil
}

// priorReadyRevisionName skips index 0, the latest-created revision being
// rolled away from, and skips revisions that never became Ready.
func priorReadyRevisionName(revs []revisionEntry) string {
	for i, r := range revs {
		if i == 0 || !r.ready {
			continue
		}
		return r.name
	}
	return ""
}

type revisionEntry struct {
	name  string
	ready bool
}

func parseRevisions(data []byte) []revisionEntry {
	var revs []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Status struct {
			Conditions []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"conditions"`
		} `json:"status"`
	}
	if json.Unmarshal(data, &revs) != nil {
		return nil
	}
	out := make([]revisionEntry, 0, len(revs))
	for _, r := range revs {
		ready := false
		for _, c := range r.Status.Conditions {
			if c.Type == "Ready" {
				ready = c.Status == "True"
				break
			}
		}
		out = append(out, revisionEntry{name: r.Metadata.Name, ready: ready})
	}
	return out
}

func parseRevisionNames(data []byte) []string {
	revs := parseRevisions(data)
	if revs == nil {
		return nil
	}
	names := make([]string, 0, len(revs))
	for _, r := range revs {
		names = append(names, r.name)
	}
	return names
}
