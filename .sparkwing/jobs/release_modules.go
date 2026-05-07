package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	sw "github.com/sparkwing-dev/sparkwing/sparkwing"
)

// ReleaseModulesInputs are the typed CLI flags for `wing release-modules`.
//
// Note: --dry-run is reserved by `sparkwing run` (IMP-014) and would be
// intercepted before reaching this pipeline, so we use --preview for the
// "show what would happen" mode.
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
		{Comment: "Cut a v0.1.0 baseline release", Command: "wing release-modules --version v0.1.0"},
		{Comment: "Preview without creating or pushing tags", Command: "wing release-modules --version v0.2.0 --preview"},
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
	// Tag only sub-modules, never the parent path. A `vX.Y.Z` tag at
	// the repo root would expose `github.com/sparkwing-dev/sparks-core`
	// as a real module on proxy.golang.org, which trips Go's
	// matching-version heuristic during `go get sparks-core/<sub>@vX`:
	// Go fetches the parent at the same version and adds it to the
	// build list, after which iterated `go get` of additional
	// sub-modules misroutes them as sub-packages of the umbrella zip
	// (which excludes nested-module dirs by design). Sub-module tags
	// alone are everything consumers need; humans can still reference
	// the family version via release notes or any one sub-module tag.
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
