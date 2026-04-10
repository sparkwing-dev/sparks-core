package templates

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// Render renders the named template's body against the supplied
// parameter map. Validates that every required parameter is present
// and that no unknown parameters are passed -- both are surfaced as
// hard errors rather than silently producing a malformed pipeline.
//
// Default values declared in the manifest are auto-filled when the
// caller omits them, so consumers only need to pass the required +
// any-they-want-to-override subset.
//
// Helper functions exposed inside the template:
//   - quote: Go strconv.Quote-style escaping (%q) for safe Go literals.
//   - default: returns first arg if non-empty, else the fallback.
//   - capitalize: first letter uppercase only.
//   - pascal: kebab/snake case -> PascalCase, for Go identifiers
//     derived from hyphenated params (`lint-test` -> `LintTest`).
//
// Hyphens in parameter names are translated to underscores in the
// rendering context: a manifest declaring `pipeline-name` is read as
// `{{.pipeline_name}}` inside the body. The CLI flag stays hyphenated.
func Render(name string, params map[string]string) (string, error) {
	t, err := Get(name)
	if err != nil {
		return "", err
	}
	return renderTemplate(t, params)
}

// renderTemplate is the testable core of Render -- the public entry
// point handles loading, this handles validation + execution.
func renderTemplate(t Template, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	resolved := map[string]string{}

	// Walk declared parameters: fill defaults, validate required.
	// Every declared parameter ends up in `resolved` so the template
	// engine's missingkey=error fires on typo'd field references in
	// the body, not on intentionally-empty optional params.
	//
	// Explicit-empty (`--param foo=`) is honored as "no value"
	// rather than auto-falling back to the default -- consumers who
	// want the default just omit the flag. This matters for templates
	// that use `{{ if .test_cmd }}...{{ end }}` to elide a step.
	declared := map[string]bool{}
	var missing []string
	for _, p := range t.Manifest.Parameters {
		declared[p.Name] = true
		caller, present := params[p.Name]
		switch {
		case present:
			resolved[p.Name] = caller
		case p.Default != "":
			resolved[p.Name] = p.Default
		case p.Required:
			missing = append(missing, p.Name)
		default:
			// Optional with no default + not provided. Bind to empty
			// string so `{{.foo}}` renders as "" rather than the
			// missingkey=error path.
			resolved[p.Name] = ""
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("template %q requires --param for: %s", t.Manifest.Name, strings.Join(missing, ", "))
	}

	// Reject unknown params -- a typo in --param shouldn't silently
	// vanish into a no-op substitution. Caller-supplied values are
	// already merged into resolved above; this loop is just a typo
	// check.
	var unknown []string
	for k := range params {
		if !declared[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		known := make([]string, 0, len(t.Manifest.Parameters))
		for _, p := range t.Manifest.Parameters {
			known = append(known, p.Name)
		}
		return "", fmt.Errorf("template %q: unknown params %v (known: %v)", t.Manifest.Name, unknown, known)
	}

	// Go's text/template forbids identifiers with hyphens in field
	// access (`.pipeline-name` parses as `.pipeline minus .name`). We
	// translate hyphens to underscores so authors writing template
	// bodies always say `.pipeline_name` regardless of how the param
	// is spelled in the manifest. The user-facing CLI flag stays
	// hyphenated (`--param pipeline-name=foo`) -- that's the convention.
	exec := map[string]string{}
	for k, v := range resolved {
		exec[underscored(k)] = v
	}
	tmpl, err := template.New(t.Manifest.Name).
		Option("missingkey=error").
		Funcs(funcs()).
		Parse(t.Body)
	if err != nil {
		return "", fmt.Errorf("parse template %q: %w", t.Manifest.Name, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, exec); err != nil {
		return "", fmt.Errorf("render template %q: %w", t.Manifest.Name, err)
	}
	return buf.String(), nil
}

// underscored converts hyphens to underscores so templates can refer
// to a parameter via `.foo_bar` even when the manifest spells it
// `foo-bar`. This is a one-way transform -- the manifest is canonical.
func underscored(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// funcs are the helper functions exposed inside templates. Kept
// minimal: we want templates to be obvious to a reader, not a tiny
// DSL.
//
//   - quote: %q on a string. Safe Go-literal escaping.
//   - default: first non-empty arg.
//   - capitalize: just first-letter uppercase, no separator handling.
//   - pascal: kebab-or-snake case to PascalCase. Use this to derive a
//     valid Go identifier from a hyphenated parameter (struct names,
//     receiver names) -- `lint-test` -> `LintTest`.
func funcs() template.FuncMap {
	return template.FuncMap{
		"quote": func(s string) string {
			// Use %q for safe, deterministic Go-string escaping.
			return fmt.Sprintf("%q", s)
		},
		"default": func(value, fallback string) string {
			if value == "" {
				return fallback
			}
			return value
		},
		"capitalize": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"pascal": pascalCase,
	}
}

// pascalCase converts kebab/snake to PascalCase: "build-test-deploy"
// -> "BuildTestDeploy", "lint_test" -> "LintTest". Matches the
// kebabToPascal helper sparkwing's CLI scaffolder uses for struct
// names.
func pascalCase(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	upper := true
	for _, r := range s {
		if r == '-' || r == '_' {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(toUpperRune(r))
			upper = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func toUpperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
