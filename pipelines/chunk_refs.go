package pipelines

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// verifyHTMLChunkRefs scans every *.html file under outDir for local
// asset references (src="/_next/..." / href="/_next/...", and the
// non-Next /static/ equivalent) and checks that each referenced file
// exists on disk under outDir.
//
// Catches the failure mode in ISS-034: a Next.js build where
// `output: "export"` did not engage regenerates JS chunks under
// `out/_next/static/chunks/` with new content hashes but does not
// refresh `out/*.html`. The stale HTML keeps pointing at chunk
// filenames from the prior export-mode build that the current build
// did not emit. If we synced this to S3, the asset pass would
// `--delete` the live chunks (because they no longer exist locally)
// and the HTML pass would no-op, leaving live pages referencing
// chunks that 404. Failing here keeps S3 internally consistent.
//
// Only paths under known static-asset prefixes are verified. Routes
// (e.g. `/about`) and external URLs are left alone; this check is
// scoped to "files the build claims to have emitted".
func verifyHTMLChunkRefs(outDir string) error {
	htmlFiles, err := filepath.Glob(filepath.Join(outDir, "*.html"))
	if err != nil {
		return fmt.Errorf("glob html in %s: %w", outDir, err)
	}
	// Also check nested route HTML (Next-style out/<route>/index.html).
	nested, err := filepath.Glob(filepath.Join(outDir, "*", "index.html"))
	if err != nil {
		return fmt.Errorf("glob nested html in %s: %w", outDir, err)
	}
	htmlFiles = append(htmlFiles, nested...)
	if len(htmlFiles) == 0 {
		return nil
	}

	var missing []string
	for _, f := range htmlFiles {
		body, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		for _, ref := range extractStaticRefs(string(body)) {
			rel := strings.TrimPrefix(ref, "/")
			if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
				if os.IsNotExist(err) {
					missing = append(missing, fmt.Sprintf("%s -> %s", f, ref))
					continue
				}
				return fmt.Errorf("stat %s: %w", rel, err)
			}
		}
	}
	if len(missing) > 0 {
		preview := missing
		const max = 5
		if len(preview) > max {
			preview = preview[:max]
		}
		return fmt.Errorf(
			"static-deploy: %d HTML asset reference(s) missing from %s -- "+
				"the build emitted HTML pointing at files it did not produce, "+
				"likely a Next.js `output: \"export\"` config that did not engage. "+
				"First: %s",
			len(missing), outDir, strings.Join(preview, "; "),
		)
	}
	return nil
}

// staticRefRE matches src= / href= attributes whose value starts with
// a known static-asset prefix. Only single- or double-quoted values
// are matched; bare attribute values are HTML5-legal but Next/React
// emit quoted attributes.
var staticRefRE = regexp.MustCompile(
	`(?:src|href)\s*=\s*["'](?P<path>/(?:_next/static|static)/[^"'?#]+)["']`,
)

func extractStaticRefs(html string) []string {
	matches := staticRefRE.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		p := m[1]
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}
