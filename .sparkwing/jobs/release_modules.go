package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	sw "github.com/sparkwing-dev/sparkwing/sparkwing"
)

// ReleaseModulesInputs are the typed CLI flags for `sparkwing run release-modules`.
//
// Note: --dry-run is reserved by `sparkwing run` and would be intercepted
// before reaching this pipeline, so we use --preview for the "show what
// would happen" mode.
type ReleaseModulesInputs struct {
	Version string `flag:"version" desc:"Semver tag (e.g. v0.1.0) applied to root + each module" default:"v0.1.0"`
	Preview bool   `flag:"preview" desc:"Print the tags that would be created and pushed, without doing it"`
}

// ReleaseModules tags every module in spark.json at the given version
// and pushes the new tags to origin. Deliberately does NOT create a
// `vX.Y.Z` tag at the repo root -- see tagNames() for why.
type ReleaseModules struct{ sw.Base }

func (p ReleaseModules) ShortHelp() string { return "Tag and push per-module release tags" }

func (p ReleaseModules) Help() string {
	return `Reads modules from spark.json and creates a coordinated release:
<module>/v<X.Y.Z> for every module in the manifest. Pushes the new tags
to origin. The repo root is intentionally not tagged.`
}

func (ReleaseModules) Examples() []sw.Example {
	return []sw.Example{
		{Comment: "Cut a v0.1.0 baseline release", Command: "sparkwing run release-modules --version v0.1.0"},
		{Comment: "Preview without creating or pushing tags", Command: "sparkwing run release-modules --version v0.2.0 --preview"},
	}
}

func (ReleaseModules) Plan(_ context.Context, plan *sw.Plan, in ReleaseModulesInputs, run sw.RunContext) error {
	sw.Job(plan, run.Pipeline, &ReleaseModulesJob{In: in})
	return nil
}

type ReleaseModulesJob struct {
	sw.Base
	In ReleaseModulesInputs
}

func (j *ReleaseModulesJob) Work(w *sw.Work) (*sw.WorkStep, error) {
	clean := sw.Step(w, "verify-clean-tree", j.verifyClean)
	tag := sw.Step(w, "create-tags", j.createTags).Needs(clean)
	sw.Step(w, "push-tags", j.pushTags).Needs(tag)
	return nil, nil
}

var versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// refusePostV0 hard-blocks any version >= v1.0.0. While sparks-core
// is pre-1.0, every release ships under v0.x.y; stepping to v1.0.0+
// commits the API surface of every module in spark.json, which is
// a decision that must be made deliberately. Removing this gate is
// the unlock: edit refusePostV0 (or its caller in tagNames) when
// the time comes.
func refusePostV0(version string) error {
	rest := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(rest, ".", 2)
	if len(parts) < 1 || parts[0] != "0" {
		return fmt.Errorf("release-modules: version %q is v1.0.0+ but sparks-core is locked to v0.x. "+
			"Bumping to v1+ commits the API surface of every spark.json module; "+
			"if that's intentional, remove the pre-1.0 lock in .sparkwing/jobs/release_modules.go and resubmit", version)
	}
	return nil
}

type sparkManifest struct {
	Modules []struct {
		Path string `json:"path"`
	} `json:"modules"`
}

func (j *ReleaseModulesJob) tagNames() ([]string, error) {
	if !versionRE.MatchString(j.In.Version) {
		return nil, fmt.Errorf("invalid version %q: want vMAJOR.MINOR.PATCH (e.g. v0.1.0)", j.In.Version)
	}
	if err := refusePostV0(j.In.Version); err != nil {
		return nil, err
	}
	data, err := sw.ReadFile("spark.json")
	if err != nil {
		return nil, fmt.Errorf("read spark.json: %w", err)
	}
	var m sparkManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse spark.json: %w", err)
	}
	// safety: tag sub-modules only -- a vX.Y.Z tag at the repo root exposes the umbrella as a module and misroutes iterated go get.
	var tags []string
	for _, mod := range m.Modules {
		tags = append(tags, mod.Path+"/"+j.In.Version)
	}
	return tags, nil
}

func (j *ReleaseModulesJob) verifyClean(ctx context.Context) error {
	return sw.Bash(ctx, "git status --porcelain").MustBeEmpty("uncommitted changes; commit or stash before tagging")
}

func (j *ReleaseModulesJob) createTags(ctx context.Context) error {
	tags, err := j.tagNames()
	if err != nil {
		return err
	}
	for _, t := range tags {
		out, err := sw.Exec(ctx, "git", "tag", "-l", t).String()
		if err != nil {
			return err
		}
		if out != "" {
			return fmt.Errorf("tag %s already exists locally; delete it before re-running", t)
		}
	}
	if j.In.Preview {
		for _, t := range tags {
			sw.Info(ctx, "[preview] would create tag %s", t)
		}
		return nil
	}
	for _, t := range tags {
		if _, err := sw.Exec(ctx, "git", "tag", t).Run(); err != nil {
			return err
		}
		sw.Info(ctx, "created tag %s", t)
	}
	return nil
}

func (j *ReleaseModulesJob) pushTags(ctx context.Context) error {
	tags, err := j.tagNames()
	if err != nil {
		return err
	}
	if j.In.Preview {
		sw.Info(ctx, "[preview] would push %d tags to origin: %v", len(tags), tags)
		return nil
	}
	args := append([]string{"push", "origin"}, tags...)
	_, err = sw.Exec(ctx, "git", args...).Run()
	return err
}

func init() {
	sw.Register[ReleaseModulesInputs]("release-modules", func() sw.Pipeline[ReleaseModulesInputs] { return &ReleaseModules{} })
}
