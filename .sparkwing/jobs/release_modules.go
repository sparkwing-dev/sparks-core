package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	sw "github.com/sparkwing-dev/sparkwing/sparkwing"
)

// ReleaseModulesInputs are the typed CLI flags for `wing release-modules`.
type ReleaseModulesInputs struct {
	Version string `flag:"version" desc:"Semver tag (e.g. v0.1.0) applied to root + each module" default:"v0.1.0"`
	DryRun  bool   `flag:"dry-run" desc:"Plan the tagging without creating or pushing"`
}

// ReleaseModules tags every module in spark.json plus the repo root at
// the same version, then pushes the new tags to origin.
type ReleaseModules struct{ sw.Base }

func (p ReleaseModules) ShortHelp() string { return "Tag and push per-module + root release tags" }

func (p ReleaseModules) Help() string {
	return `Reads modules from spark.json and creates a coordinated release:
the root v<X.Y.Z> tag plus <module>/v<X.Y.Z> for every module. Pushes
the new tags to origin.`
}

func (ReleaseModules) Examples() []sw.Example {
	return []sw.Example{
		{Comment: "Cut a v0.1.0 baseline release", Command: "wing release-modules --version v0.1.0"},
		{Comment: "Preview without creating or pushing tags", Command: "wing release-modules --version v0.2.0 --dry-run"},
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

func (j *ReleaseModulesJob) Work() *sw.Work {
	w := sw.NewWork()
	clean := w.Step("verify-clean-tree", j.verifyClean)
	tag := w.Step("create-tags", j.createTags).Needs(clean)
	w.Step("push-tags", j.pushTags).Needs(tag)
	return w
}

var versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

type sparkManifest struct {
	Modules []struct {
		Path string `json:"path"`
	} `json:"modules"`
}

func (j *ReleaseModulesJob) tagNames() ([]string, error) {
	if !versionRE.MatchString(j.In.Version) {
		return nil, fmt.Errorf("invalid version %q: want vMAJOR.MINOR.PATCH (e.g. v0.1.0)", j.In.Version)
	}
	data, err := sw.ReadFile("spark.json")
	if err != nil {
		return nil, fmt.Errorf("read spark.json: %w", err)
	}
	var m sparkManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse spark.json: %w", err)
	}
	tags := []string{j.In.Version}
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
	if j.In.DryRun {
		for _, t := range tags {
			sw.Info(ctx, "[dry-run] would create tag %s", t)
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
	if j.In.DryRun {
		sw.Info(ctx, "[dry-run] would push %d tags to origin: %v", len(tags), tags)
		return nil
	}
	args := append([]string{"push", "origin"}, tags...)
	_, err = sw.Exec(ctx, "git", args...).Run()
	return err
}

func init() {
	sw.Register[ReleaseModulesInputs]("release-modules", func() sw.Pipeline[ReleaseModulesInputs] { return &ReleaseModules{} })
}
