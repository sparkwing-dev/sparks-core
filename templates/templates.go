// Package templates exposes a curated pipeline template registry as
// an embed.FS plus typed accessors over the manifests. Templates are
// the durable artifact -- the sparkwing CLI's `pipeline new --template`
// flag is one consumer; agents are another.
//
// Each template is a directory with three files:
//   - template.yaml   -- manifest (Manifest type below)
//   - pipeline.go.tmpl -- Go text/template body to render
//   - README.md       -- prose description for humans + agents
//
// The registry is intentionally small. Adding a template: drop the
// directory, append the name to templateNames below, write a
// CHANGELOG entry. Each template should be the simplified canonical
// version of a real production pattern.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"go.yaml.in/yaml/v3"
)

// FS is the embedded template registry. Walking it yields every
// template directory and its files. Consumers that just want the
// raw bytes (for rendering or diffing) reach for FS directly.
//
//go:embed all:static-deploy-s3-cloudfront all:static-deploy-gcs-cloudcdn all:docker-deploy-ecr-eks all:docker-deploy-gar-gke all:approval-gated-deploy all:next-build-and-push all:build-publish-binary all:docker-build-smoketest all:lint-test-go all:test-shards all:integration-test-with-service all:scheduled-cleanup all:go-test-build-deploy-k8s all:go-test-migrate-deploy-argo all:container-deploy-ecs-fargate all:docker-deploy-gar-cloudrun all:cloudrun-deploy-source all:gke-deploy-gar-kubectl all:lambda-deploy all:cloud-functions-deploy all:next-preview-deploy-cloudrun all:canary-deploy-k8s all:github-release-go all:npm-publish-package all:pypi-publish-wheel all:container-publish-multiarch all:lint-test-node all:lint-test-python all:test-matrix all:coverage-gated-test all:cached-test-suite all:skip-if-paths-unchanged all:docker-build-layer-cache all:terraform-plan-pr all:terraform-apply-gated all:db-migrate-updown all:db-backup-restore-drill all:scheduled-db-backup
var FS embed.FS

// templateNames is the canonical list of templates in this registry.
// Order matters: List() returns them in this order, which is the
// human-friendly grouping (cloud parity pairs together, build-only
// next, ci-hygiene last).
var templateNames = []string{
	"static-deploy-s3-cloudfront",
	"static-deploy-gcs-cloudcdn",
	"docker-deploy-ecr-eks",
	"docker-deploy-gar-gke",
	"go-test-build-deploy-k8s",
	"go-test-migrate-deploy-argo",
	"approval-gated-deploy",
	"next-build-and-push",
	"build-publish-binary",
	"docker-build-smoketest",
	"lint-test-go",
	"test-shards",
	"integration-test-with-service",
	"scheduled-cleanup",
	"container-deploy-ecs-fargate",
	"docker-deploy-gar-cloudrun",
	"cloudrun-deploy-source",
	"gke-deploy-gar-kubectl",
	"lambda-deploy",
	"cloud-functions-deploy",
	"next-preview-deploy-cloudrun",
	"canary-deploy-k8s",
	"github-release-go",
	"npm-publish-package",
	"pypi-publish-wheel",
	"container-publish-multiarch",
	"lint-test-node",
	"lint-test-python",
	"test-matrix",
	"coverage-gated-test",
	"cached-test-suite",
	"skip-if-paths-unchanged",
	"docker-build-layer-cache",
	"terraform-plan-pr",
	"terraform-apply-gated",
	"db-migrate-updown",
	"db-backup-restore-drill",
	"scheduled-db-backup",
}

// Parameter declares one substitution variable for a template. Authors
// pass values via `--param name=value` on the CLI; agents pass them
// however they like as long as required parameters are present.
type Parameter struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Applicability records the template's intended scope. Empty values
// mean "no constraint" (the template works anywhere). The cloud /
// category fields are advisory -- the CLI doesn't refuse to render
// a template against a "wrong" repo, it just surfaces them in
// `templates show` so authors pick the right starter.
type Applicability struct {
	Cloud    []string `yaml:"cloud,omitempty" json:"cloud,omitempty"`
	Category string   `yaml:"category,omitempty" json:"category,omitempty"`
}

// Verification tiers. A manifest's Verify field records how far the
// registry verification harness can exercise a scaffold of the
// template without cloud credentials or live infrastructure.
const (
	// VerifyRunnable means the scaffolded pipeline runs green on a
	// laptop with no cloud credentials (a Docker daemon is permitted).
	VerifyRunnable = "runnable"
	// VerifyDryRunnable means a side-effect-free run path exists --
	// e.g. a preview/plan parameter -- that runs green locally.
	VerifyDryRunnable = "dry-runnable"
	// VerifyCompileOnly means the template touches real cloud services,
	// so the harness can only render, compile, lint, and explain it.
	VerifyCompileOnly = "compile-only"
)

// Verification fixtures. A manifest's VerifyFixture field names the
// scratch-repo scaffolding the harness synthesizes before a run.
const (
	// FixtureNone is an empty scratch repo (just the scaffolded pipeline).
	FixtureNone = "none"
	// FixtureGoModule is a go.mod plus a trivial buildable package and a
	// passing test at the scratch repo root, for templates whose steps
	// run go build / vet / test there.
	FixtureGoModule = "go-module"
	// FixtureDocker is the go-module contents plus a Dockerfile, for
	// templates whose steps build or run a container image.
	FixtureDocker = "docker"
	// FixtureNodeModule is a package.json with a passing test script,
	// for templates whose steps run npm / node tooling at the scratch
	// repo root.
	FixtureNodeModule = "node-module"
	// FixturePythonModule is a pyproject.toml plus a trivial package and
	// a passing test, for templates whose steps run python tooling at
	// the scratch repo root.
	FixturePythonModule = "python-module"
	// FixturePostgres is the go-module contents plus an ephemeral
	// Postgres the harness provisions, its DSN injected as the
	// DATABASE_URL secret, for templates that migrate or query a live
	// database.
	FixturePostgres = "postgres"
)

// Manifest is the parsed template.yaml shape. Name + Description are
// the only required fields; everything else is opt-in metadata that
// templates use to communicate constraints.
type Manifest struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// WhenToUse is the catalog signal: a one-or-two-line answer to
	// "which template do I pick?", written for an agent choosing among
	// starters. Distinct from Description (what it does) -- this is when
	// to reach for it versus a sibling.
	WhenToUse string `yaml:"whenToUse,omitempty" json:"whenToUse,omitempty"`
	// Prerequisite is what must already exist in the repo for a scaffold
	// of this template to `sparkwing run` successfully -- e.g. "a Go
	// module at the repo root". Surfaced by `pipeline templates` and
	// printed after `pipeline new` so the first run isn't a surprise.
	Prerequisite  string        `yaml:"prerequisite,omitempty" json:"prerequisite,omitempty"`
	Parameters    []Parameter   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Applicability Applicability `yaml:"applicability,omitempty" json:"applicability,omitempty"`
	// Verify is the verification tier (VerifyRunnable / VerifyDryRunnable
	// / VerifyCompileOnly) the registry harness applies to a scaffold of
	// this template. Defaults to VerifyCompileOnly when absent; read it
	// through Tier() to get the resolved value.
	Verify string `yaml:"verify,omitempty" json:"verify,omitempty"`
	// VerifyParams supplies a sample value for each parameter the harness
	// scaffolds with. Every required parameter must have an entry; values
	// are safe placeholders (fake bucket names, example.com URLs) chosen
	// so a render/compile/lint/explain never reaches real infrastructure.
	VerifyParams map[string]string `yaml:"verify_params,omitempty" json:"verify_params,omitempty"`
	// VerifyFixture names the scratch-repo scaffolding the harness
	// synthesizes before a runnable/dry-runnable run (FixtureNone /
	// FixtureGoModule / FixtureDocker / FixtureNodeModule /
	// FixturePythonModule / FixturePostgres). Ignored for the
	// compile-only tier. Defaults to FixtureNone; read it through
	// Fixture().
	VerifyFixture string `yaml:"verify_fixture,omitempty" json:"verify_fixture,omitempty"`
	// VerifyTools lists host commands a runnable/dry-runnable run needs
	// beyond the fixture's own toolchain (e.g. migrate, pg_dump). The
	// harness skips the run step, staying green, when one is missing;
	// "docker" means a reachable daemon, not just the binary.
	VerifyTools []string `yaml:"verify_tools,omitempty" json:"verify_tools,omitempty"`
}

// Tier returns the manifest's verification tier, defaulting to
// VerifyCompileOnly when the manifest leaves Verify unset.
func (m Manifest) Tier() string {
	if m.Verify == "" {
		return VerifyCompileOnly
	}
	return m.Verify
}

// Fixture returns the manifest's verification fixture, defaulting to
// FixtureNone when the manifest leaves VerifyFixture unset.
func (m Manifest) Fixture() string {
	if m.VerifyFixture == "" {
		return FixtureNone
	}
	return m.VerifyFixture
}

// Template bundles a manifest with its on-disk artifacts. ReadMe is
// the contents of README.md; Body is the contents of pipeline.go.tmpl
// pre-rendering. Consumers that want the rendered body call Render.
type Template struct {
	Manifest Manifest `json:"manifest"`
	ReadMe   string   `json:"readme,omitempty"`
	Body     string   `json:"body,omitempty"`
}

// List returns every registered template in canonical order. Used by
// `sparkwing pipeline templates` (with --json producing this exact
// shape) and by agents enumerating starters.
func List() ([]Template, error) {
	out := make([]Template, 0, len(templateNames))
	for _, name := range templateNames {
		t, err := Get(name)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ListNames returns just the template names. Cheaper than List when
// the caller is only doing existence checks.
func ListNames() []string {
	out := make([]string, len(templateNames))
	copy(out, templateNames)
	return out
}

// Get loads one template by name. Returns an error wrapping
// fs.ErrNotExist when the name doesn't match any registered template.
func Get(name string) (Template, error) {
	if !known(name) {
		return Template{}, fmt.Errorf("unknown template %q (known: %v): %w", name, templateNames, fs.ErrNotExist)
	}
	manifest, err := readManifest(name)
	if err != nil {
		return Template{}, err
	}
	body, err := fs.ReadFile(FS, path.Join(name, "pipeline.go.tmpl"))
	if err != nil {
		return Template{}, fmt.Errorf("read body for %s: %w", name, err)
	}
	readme, err := fs.ReadFile(FS, path.Join(name, "README.md"))
	if err != nil {
		// README is required: every template must explain itself.
		return Template{}, fmt.Errorf("read README for %s: %w", name, err)
	}
	return Template{
		Manifest: manifest,
		Body:     string(body),
		ReadMe:   string(readme),
	}, nil
}

// readManifest parses template.yaml for one template name.
func readManifest(name string) (Manifest, error) {
	raw, err := fs.ReadFile(FS, path.Join(name, "template.yaml"))
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest for %s: %w", name, err)
	}
	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest for %s: %w", name, err)
	}
	if m.Name == "" {
		// Defensive: a template directory with an empty manifest is
		// almost certainly an authoring mistake. Fail loudly so the
		// `templates` test catches it during PR review.
		return Manifest{}, fmt.Errorf("manifest for %s has empty name", name)
	}
	if m.Name != name {
		return Manifest{}, fmt.Errorf("manifest name %q for %s mismatches directory name", m.Name, name)
	}
	if err := validateVerification(m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// validateVerification enforces the verification-metadata contract:
// the tier must be a known value, the fixture (when set) must be a
// known value, every required parameter must have a verify_params
// sample, and verify_params may only reference declared parameters.
func validateVerification(m Manifest) error {
	switch m.Tier() {
	case VerifyRunnable, VerifyDryRunnable, VerifyCompileOnly:
	default:
		return fmt.Errorf("manifest for %s: unknown verify %q (want %s|%s|%s)",
			m.Name, m.Verify, VerifyRunnable, VerifyDryRunnable, VerifyCompileOnly)
	}
	switch m.Fixture() {
	case FixtureNone, FixtureGoModule, FixtureDocker, FixtureNodeModule, FixturePythonModule, FixturePostgres:
	default:
		return fmt.Errorf("manifest for %s: unknown verify_fixture %q (want %s|%s|%s|%s|%s|%s)",
			m.Name, m.VerifyFixture, FixtureNone, FixtureGoModule, FixtureDocker,
			FixtureNodeModule, FixturePythonModule, FixturePostgres)
	}
	declared := map[string]bool{}
	for _, p := range m.Parameters {
		declared[p.Name] = true
		if p.Required {
			if _, ok := m.VerifyParams[p.Name]; !ok {
				return fmt.Errorf("manifest for %s: required parameter %q has no verify_params entry", m.Name, p.Name)
			}
		}
	}
	var unknown []string
	for k := range m.VerifyParams {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("manifest for %s: verify_params references undeclared parameter(s) %v", m.Name, unknown)
	}
	return nil
}

func known(name string) bool {
	i := sort.SearchStrings(sortedNames(), name)
	sn := sortedNames()
	return i < len(sn) && sn[i] == name
}

// sortedNames returns templateNames sorted -- used by known() for
// O(log n) membership check. Cached on first call.
var sortedNamesCache []string

func sortedNames() []string {
	if sortedNamesCache != nil {
		return sortedNamesCache
	}
	out := make([]string, len(templateNames))
	copy(out, templateNames)
	sort.Strings(out)
	sortedNamesCache = out
	return out
}
