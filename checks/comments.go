package checks

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

var commentTagRE = regexp.MustCompile(`(?i)^// ?(hack|safety|bug|perf):`)

// commentOutputRE matches the Go testable-example output markers
// recognized by the testing package: "// Output:" and "// Unordered
// output:".
var commentOutputRE = regexp.MustCompile(`(?i)^// (Unordered output|Output):`)

var commentSkipDirs = map[string]bool{
	"vendor":          true,
	"testdata":        true,
	"node_modules":    true,
	".git":            true,
	".claude-scratch": true,
}

type commentViolation struct {
	file string
	line int
	text string
}

// Comments enforces the repo comment policy on every .go file under the
// given paths (default: the repo root). Two kinds of comment are
// allowed: godoc attached to a top-level declaration (package, func,
// type, const, var, import) or to a struct field / interface method;
// and a small allowlist of tagged implementation comments -- // hack:,
// // safety:, // bug:, // perf: -- each one short line that justifies
// its own existence. Compiler directives (//go:build, //go:embed,
// //nolint:...) are always allowed. Everything else -- free-floating
// comments, body narration, section dividers, and "what" comments that
// restate the code -- fails the check.
//
// Paths are resolved relative to sparkwing.WorkDir() so the check works
// from a compiled pipeline binary whose cwd is not the repo root. Pass
// specific subdirectories to scope the gate -- for example, to exclude a
// directory that is mid-rewrite -- and drop the argument to cover the
// whole tree.
func Comments(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return step.Run(ctx, "comments", func(ctx context.Context) error {
		root := sparkwing.WorkDir()
		if root == "" {
			root = "."
		}
		var violations []commentViolation
		for _, p := range paths {
			v, err := scanComments(filepath.Join(root, p))
			if err != nil {
				return err
			}
			violations = append(violations, v...)
		}
		if len(violations) == 0 {
			return nil
		}
		lines := make([]string, len(violations))
		for i, v := range violations {
			lines[i] = fmt.Sprintf("%s:%d: disallowed comment: %s", v.file, v.line, v.text)
		}
		sort.Strings(lines)
		for _, l := range lines {
			sparkwing.Info(ctx, "  %s", l)
		}
		return fmt.Errorf("%d disallowed comment(s); allowed: godoc, // hack:, // safety:, // bug:, // perf:", len(violations))
	})
}

func scanComments(root string) ([]commentViolation, error) {
	var violations []commentViolation
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if commentSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		v, perr := checkCommentsFile(path)
		if perr != nil {
			return nil
		}
		violations = append(violations, v...)
		return nil
	})
	return violations, err
}

func checkCommentsFile(path string) ([]commentViolation, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	allowed := map[*ast.CommentGroup]bool{}
	markComment(allowed, f.Doc)
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			markComment(allowed, d.Doc)
			if d.Name != nil && strings.HasPrefix(d.Name.Name, "Example") && d.Body != nil {
				bodyStart := d.Body.Lbrace
				bodyEnd := d.Body.Rbrace
				for _, cg := range f.Comments {
					if cg.Pos() >= bodyStart && cg.Pos() <= bodyEnd && commentOutputRE.MatchString(cg.List[0].Text) {
						markComment(allowed, cg)
					}
				}
			}
		case *ast.GenDecl:
			markComment(allowed, d.Doc)
			for _, spec := range d.Specs {
				collectCommentSpec(allowed, spec)
			}
		}
	}

	var out []commentViolation
	for _, cg := range f.Comments {
		if allowed[cg] {
			continue
		}
		first := cg.List[0].Text
		if isCommentDirective(first) || commentTagRE.MatchString(first) {
			continue
		}
		pos := fset.Position(cg.Pos())
		out = append(out, commentViolation{pos.Filename, pos.Line, firstCommentLine(first)})
	}
	return out, nil
}

// collectCommentSpec marks godoc attached to a top-level spec and, for
// type specs, recurses into struct fields and interface methods so their
// godoc survives. It never descends into function bodies -- comments
// there are implementation comments and must earn their place through
// the tag allowlist.
func collectCommentSpec(allowed map[*ast.CommentGroup]bool, spec ast.Spec) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		markComment(allowed, s.Doc)
		markComment(allowed, s.Comment)
		collectCommentType(allowed, s.Type)
	case *ast.ValueSpec:
		markComment(allowed, s.Doc)
		markComment(allowed, s.Comment)
	case *ast.ImportSpec:
		markComment(allowed, s.Doc)
		markComment(allowed, s.Comment)
	}
}

func collectCommentType(allowed map[*ast.CommentGroup]bool, expr ast.Expr) {
	switch t := expr.(type) {
	case *ast.StructType:
		for _, fld := range t.Fields.List {
			markComment(allowed, fld.Doc)
			markComment(allowed, fld.Comment)
			collectCommentType(allowed, fld.Type)
		}
	case *ast.InterfaceType:
		for _, m := range t.Methods.List {
			markComment(allowed, m.Doc)
			markComment(allowed, m.Comment)
		}
	case *ast.StarExpr:
		collectCommentType(allowed, t.X)
	case *ast.ArrayType:
		collectCommentType(allowed, t.Elt)
	case *ast.MapType:
		collectCommentType(allowed, t.Key)
		collectCommentType(allowed, t.Value)
	}
}

func markComment(allowed map[*ast.CommentGroup]bool, cg *ast.CommentGroup) {
	if cg != nil {
		allowed[cg] = true
	}
}

// isCommentDirective reports whether a //-comment is a compiler
// directive such as //go:build, //go:embed, or //nolint:all -- the form
// is //word:rest with no space after the slashes. The required leading
// space in "// hack:" is what keeps human tags from being mistaken for
// directives, and vice versa.
func isCommentDirective(text string) bool {
	s, ok := strings.CutPrefix(text, "//")
	if !ok || s == "" || s[0] == ' ' {
		return false
	}
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return false
	}
	for _, r := range s[:i] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func firstCommentLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[:i]
	}
	if len(text) > 80 {
		text = text[:77] + "..."
	}
	return text
}
