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

type NotesConfig struct {
	// Path defaults to "CHANGELOG.md".
	Path string
	// Version empty selects the top-most released section, the first "## "
	// heading that is not "Unreleased".
	Version string
}

// ChangelogEntry returns the notes body for one version of a Keep a
// Changelog file, along with the version its section is for.
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

var sectionHeading = regexp.MustCompile(`(?m)^##[ \t]+(.+?)[ \t]*$`)

var headingVersionToken = regexp.MustCompile(`v?\d+\.\d+\.\d+[0-9A-Za-z.\-+]*|(?i:unreleased)`)

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

func normalizeVersionToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]")
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	return strings.ToLower(s)
}
