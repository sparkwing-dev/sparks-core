package pipelines

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// verifyHTMLChunkRefs checks that every static-asset path the built HTML
// references exists on disk. A Next.js build whose `output: "export"` did
// not engage emits fresh chunk hashes without refreshing the HTML; syncing
// that to S3 would --delete the live chunks the stale HTML still points at.
// Routes and external URLs are not verified.
func verifyHTMLChunkRefs(outDir string) error {
	htmlFiles, err := filepath.Glob(filepath.Join(outDir, "*.html"))
	if err != nil {
		return fmt.Errorf("glob html in %s: %w", outDir, err)
	}
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

// staticRefRE matches only quoted attribute values; bare ones are
// HTML5-legal but Next and React emit quoted attributes.
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
