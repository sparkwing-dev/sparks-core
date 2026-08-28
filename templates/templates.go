// Package templates exposes a curated pipeline template registry as an
// embed.FS plus typed accessors over the manifests. Each template is a
// directory holding template.yaml, pipeline.go.tmpl, and README.md.
package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"go.yaml.in/yaml/v3"
)

// FS is the embedded template registry.
//
//go:embed all:static-deploy-s3-cloudfront all:static-deploy-gcs-cloudcdn all:docker-deploy-ecr-eks all:docker-deploy-gar-gke all:approval-gated-deploy all:next-build-and-push all:build-publish-binary all:docker-build-smoketest all:lint-test-go all:test-shards all:integration-test-with-service all:scheduled-cleanup all:go-test-migrate-deploy-argo all:container-deploy-ecs-fargate all:docker-deploy-gar-cloudrun all:cloudrun-deploy-source all:gke-deploy-gar-kubectl all:lambda-deploy all:cloud-functions-deploy all:next-preview-deploy-cloudrun all:github-release-go all:npm-publish-package all:pypi-publish-wheel all:container-publish-multiarch all:lint-test-node all:lint-test-python all:test-matrix all:coverage-gated-test all:cached-test-suite all:skip-if-paths-unchanged all:go-affected-tests all:docker-build-layer-cache all:terraform-plan-pr all:terraform-apply-gated all:db-migrate-updown all:db-backup-restore-drill all:scheduled-db-backup
var FS embed.FS

// templateNames is ordered for human reading -- cloud parity pairs
// together, build-only next, ci-hygiene last -- and List preserves it.
var templateNames = []string{
	"static-deploy-s3-cloudfront",
	"static-deploy-gcs-cloudcdn",
	"docker-deploy-ecr-eks",
	"docker-deploy-gar-gke",
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
	"go-affected-tests",
	"docker-build-layer-cache",
	"terraform-plan-pr",
	"terraform-apply-gated",
	"db-migrate-updown",
	"db-backup-restore-drill",
	"scheduled-db-backup",
}

// Parameter declares one substitution variable for a template.
type Parameter struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Default     string `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Applicability records the template's intended scope. It is advisory:
// empty means no constraint, and nothing refuses a mismatched render.
type Applicability struct {
	Cloud    []string `yaml:"cloud,omitempty" json:"cloud,omitempty"`
	Category string   `yaml:"category,omitempty" json:"category,omitempty"`
}

// Verification tiers for Manifest.Verify: how far the registry harness can
// exercise a scaffold of the template without cloud credentials.
const (
	// VerifyRunnable runs green locally; a Docker daemon is permitted.
	VerifyRunnable = "runnable"
	// VerifyDryRunnable has a side-effect-free path that runs green locally.
	VerifyDryRunnable = "dry-runnable"
	// VerifyCompileOnly can only be rendered, compiled, linted, and explained.
	VerifyCompileOnly = "compile-only"
)

// Verification fixtures for Manifest.VerifyFixture: the scratch-repo
// scaffolding the harness synthesizes before a run.
const (
	// FixtureNone is an empty scratch repo.
	FixtureNone = "none"
	// FixtureGoModule is a go.mod plus a buildable package and a passing test.
	FixtureGoModule = "go-module"
	// FixtureDocker is FixtureGoModule plus a Dockerfile.
	FixtureDocker = "docker"
	// FixtureNodeModule is a package.json with a passing test script.
	FixtureNodeModule = "node-module"
	// FixturePythonModule is a pyproject.toml plus a package and a passing test.
	FixturePythonModule = "python-module"
	// FixturePostgres is FixtureGoModule plus an ephemeral Postgres whose DSN
	// is injected as the DATABASE_URL secret.
	FixturePostgres = "postgres"
)

// Manifest is the parsed template.yaml shape. Only Name and Description
// are required.
type Manifest struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// WhenToUse says when to reach for this template over a sibling, where
	// Description says what it does.
	WhenToUse string `yaml:"whenToUse,omitempty" json:"whenToUse,omitempty"`
	// Prerequisite is what must already exist in the repo for a scaffold to
	// run, e.g. "a Go module at the repo root".
	Prerequisite  string        `yaml:"prerequisite,omitempty" json:"prerequisite,omitempty"`
	Parameters    []Parameter   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Applicability Applicability `yaml:"applicability,omitempty" json:"applicability,omitempty"`
	// Verify is the verification tier; read it through Tier, which supplies
	// the VerifyCompileOnly default.
	Verify string `yaml:"verify,omitempty" json:"verify,omitempty"`
	// VerifyParams holds a sample value per parameter, required for every
	// required parameter. Values are placeholders that never reach real
	// infrastructure.
	VerifyParams map[string]string `yaml:"verify_params,omitempty" json:"verify_params,omitempty"`
	// VerifyFixture is ignored for the compile-only tier; read it through
	// Fixture, which supplies the FixtureNone default.
	VerifyFixture string `yaml:"verify_fixture,omitempty" json:"verify_fixture,omitempty"`
	// VerifyTools lists host commands a run needs beyond the fixture's own
	// toolchain. A missing one skips the run step rather than failing it;
	// "docker" means a reachable daemon, not just the binary.
	VerifyTools []string `yaml:"verify_tools,omitempty" json:"verify_tools,omitempty"`
}

// Tier returns Verify, defaulting to VerifyCompileOnly.
func (m Manifest) Tier() string {
	if m.Verify == "" {
		return VerifyCompileOnly
	}
	return m.Verify
}

// Fixture returns VerifyFixture, defaulting to FixtureNone.
func (m Manifest) Fixture() string {
	if m.VerifyFixture == "" {
		return FixtureNone
	}
	return m.VerifyFixture
}

// Template bundles a manifest with its README and its unrendered
// pipeline.go.tmpl body.
type Template struct {
	Manifest Manifest `json:"manifest"`
	ReadMe   string   `json:"readme,omitempty"`
	Body     string   `json:"body,omitempty"`
}

// List returns every registered template in canonical order.
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

// ListNames returns just the template names, without reading their files.
func ListNames() []string {
	out := make([]string, len(templateNames))
	copy(out, templateNames)
	return out
}

// Get loads one template by name, wrapping fs.ErrNotExist for an unknown one.
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
		return Template{}, fmt.Errorf("read README for %s: %w", name, err)
	}
	return Template{
		Manifest: manifest,
		Body:     string(body),
		ReadMe:   string(readme),
	}, nil
}

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
