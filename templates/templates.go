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
//go:embed all:static-deploy-s3-cloudfront all:static-deploy-gcs-cloudcdn all:docker-deploy-ecr-eks all:docker-deploy-gar-gke all:next-build-and-push all:lint-test-go all:go-test-build-deploy-k8s all:go-test-migrate-deploy-argo
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
	"next-build-and-push",
	"lint-test-go",
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
	WhenToUse     string        `yaml:"whenToUse,omitempty" json:"whenToUse,omitempty"`
	Parameters    []Parameter   `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Applicability Applicability `yaml:"applicability,omitempty" json:"applicability,omitempty"`
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
	return m, nil
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
