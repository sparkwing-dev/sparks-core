package jobs

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

// moduleLayer is the layer each module may import strictly downward from:
// 0 leaves, 1 capability blocks, 2 orchestrators, 3 high-level primitives.
// An unlisted module's imports are not checked at all, so a new one
// silently escapes the rule until it is added here.
var moduleLayer = map[string]int{
	"step": 0, "aws": 0, "probe": 0, "templates": 0,
	"contentkey": 0,
	"docker":     1, "s3": 1, "kube": 1, "gitops": 1,
	"migrate": 1, "services": 1, "notify": 1, "checks": 1,
	"gcp": 1, "ecs": 1, "lambda": 1, "release": 1,
	"coverage": 1, "terraform": 1, "dbbackup": 1,
	"deploy": 2, "rollback": 2, "cloudrun": 2,
	"pipelines": 3,
}

const (
	sparksCorePrefix = "github.com/sparkwing-dev/sparks-core/"
	sparkwingSDK     = "github.com/sparkwing-dev/sparkwing"
)

// checkModuleLayering also refuses an SDK import from templates, which the
// sparkwing CLI depends on as an SDK-free leaf.
func checkModuleLayering(ctx context.Context) error {
	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}
	files, err := sparkwing.Bash(ctx, "git ls-files '*.go'").Lines()
	if err != nil {
		return err
	}
	var offenders []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		module := firstPathSegment(f)
		if _, layered := moduleLayer[module]; !layered {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		imports, err := importPaths(data)
		if err != nil {
			continue
		}
		for _, imp := range imports {
			if msg := layeringViolation(module, imp); msg != "" {
				offenders = append(offenders, fmt.Sprintf("%s: %s", f, msg))
			}
		}
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf("module layering violation(s) -- sparks-core modules import strictly downward only:\n    %s",
			strings.Join(offenders, "\n    "))
	}
	return nil
}

func layeringViolation(module, importPath string) string {
	myLayer, known := moduleLayer[module]
	if !known {
		return ""
	}
	if module == "templates" && isSparkwingSDK(importPath) {
		return "templates must not import the sparkwing SDK (the CLI depends on it as an SDK-free leaf)"
	}
	if !strings.HasPrefix(importPath, sparksCorePrefix) {
		return ""
	}
	dep := importPath[len(sparksCorePrefix):]
	if i := strings.IndexByte(dep, '/'); i >= 0 {
		dep = dep[:i]
	}
	depLayer, ok := moduleLayer[dep]
	if !ok || dep == module {
		return ""
	}
	if depLayer >= myLayer {
		return fmt.Sprintf("imports %s (layer %d) but is layer %d -- may only import strictly lower layers", dep, depLayer, myLayer)
	}
	return ""
}

// isSparkwingSDK excludes sparks-core, which shares the org prefix.
func isSparkwingSDK(importPath string) bool {
	if strings.HasPrefix(importPath, sparksCorePrefix) {
		return false
	}
	return importPath == sparkwingSDK || strings.HasPrefix(importPath, sparkwingSDK+"/")
}

func importPaths(src []byte) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func firstPathSegment(p string) string {
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}
