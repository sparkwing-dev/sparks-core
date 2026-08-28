package templates

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/template"
)

// Render renders the named template's body against params, auto-filling
// manifest defaults and erroring on a missing required or unknown
// parameter. Hyphens in parameter names become underscores in the
// rendering context: `pipeline-name` is read as `{{.pipeline_name}}`.
// See funcs for the helpers templates can call.
func Render(name string, params map[string]string) (string, error) {
	t, err := Get(name)
	if err != nil {
		return "", err
	}
	return renderTemplate(t, params)
}

// renderTemplate puts every declared parameter in resolved so
// missingkey=error fires on a typo'd field reference rather than on an
// intentionally-empty optional param. An explicit `--param foo=` means "no
// value" rather than the default, so templates can elide a step with
// `{{ if .test_cmd }}`.
func renderTemplate(t Template, params map[string]string) (string, error) {
	if params == nil {
		params = map[string]string{}
	}
	resolved := map[string]string{}

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
			resolved[p.Name] = ""
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("template %q requires --param for: %s", t.Manifest.Name, strings.Join(missing, ", "))
	}

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

// underscored exists because text/template parses `.foo-bar` as subtraction.
// It is one-way: the hyphenated manifest name stays canonical.
func underscored(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// funcs are the helpers exposed inside templates: quote (%q), default
// (first non-empty), capitalize (first letter only), and pascal
// (kebab-or-snake to a Go identifier).
func funcs() template.FuncMap {
	return template.FuncMap{
		"quote": func(s string) string {
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
