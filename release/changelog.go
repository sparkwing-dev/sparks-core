package release

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// NotesConfig locates a changelog and the section to extract from it.
type NotesConfig struct {
	// Path is the changelog file, relative to the repo root. Defaults to
	// "CHANGELOG.md".
	Path string
	// Version, when set, selects the section for that exact version (with
	// or without a leading "v"). When empty, the top-most released section
	// is used (the first "## " heading that is not "Unreleased").
	Version string
}

// ChangelogEntry extracts the release notes for one version out of a
// Keep a Changelog file and returns the notes body together with the
// version the section is for. With NotesConfig.Version empty it returns
// the top released section, so a caller that just derived a version can
// still discover which section it landed on.
//
// It reads a local file only and always runs for real.
func ChangelogEntry(ctx context.Context, cfg NotesConfig) (notes, version string, err error) {
	path := cfg.Path
	if path == "" {
		path = "CHANGELOG.md"
	}
	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return "", "", fmt.Errorf("release: read %s: %w", path, err)
	}

	err = step.Run(ctx, "changelog notes", func(ctx context.Context) error {
		n, v, perr := parseChangelog(string(data), cfg.Version)
		if perr != nil {
			return perr
		}
		notes, version = n, v
		sparkwing.Info(ctx, "changelog section for %s (%d bytes of notes)", version, len(notes))
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return notes, version, nil
}

// sectionHeading matches a level-2 changelog heading and captures the
// text after "## ". Both "## [1.2.0] - 2024-01-01" and "## 1.2.0" forms
// are captured; the version token is pulled from the captured text by
// headingVersion.
var sectionHeading = regexp.MustCompile(`(?m)^##[ \t]+(.+?)[ \t]*$`)

// headingVersionToken pulls the first semver-or-Unreleased token out of a
// heading's text, tolerating surrounding brackets and a trailing date.
var headingVersionToken = regexp.MustCompile(`v?\d+\.\d+\.\d+[0-9A-Za-z.\-+]*|(?i:unreleased)`)

// parseChangelog extracts the notes body and version for wantVersion, or
// for the top-most released (non-Unreleased) section when wantVersion is
// empty. Version matching ignores a leading "v" and the surrounding
// brackets of the Keep a Changelog "## [x.y.z]" form.
func parseChangelog(content, wantVersion string) (notes, version string, err error) {
	locs := sectionHeading.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return "", "", fmt.Errorf("release: no '## ' sections found in changelog")
	}

	want := normalizeVersionToken(wantVersion)
	for i, loc := range locs {
		headText := content[loc[2]:loc[3]]
		tok := headingVersionToken.FindString(headText)
		if tok == "" {
			continue
		}
		norm := normalizeVersionToken(tok)
		if want == "" {
			if strings.EqualFold(norm, "unreleased") {
				continue
			}
		} else if norm != want {
			continue
		}

		bodyStart := loc[1]
		bodyEnd := len(content)
		if i+1 < len(locs) {
			bodyEnd = locs[i+1][0]
		}
		body := strings.Trim(content[bodyStart:bodyEnd], "\n")
		body = strings.TrimRight(body, " \t\n")
		return body, tok, nil
	}

	if want == "" {
		return "", "", fmt.Errorf("release: no released section found in changelog (only Unreleased?)")
	}
	return "", "", fmt.Errorf("release: no changelog section for version %q", wantVersion)
}

// normalizeVersionToken lowercases and strips a single leading "v" plus
// any bracket/space decoration so "v1.2.3", "1.2.3", and "[1.2.0]"
// compare on their bare version.
func normalizeVersionToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	return strings.ToLower(s)
}
