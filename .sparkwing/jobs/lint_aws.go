package jobs

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

var awsWordRe = regexp.MustCompile(`\baws\b`)

// awsProfileResolvers make an aws call target an explicit account, or
// correctly no profile under IRSA. CallerIdentityArgs counts because it
// appends ProfileArgs itself.
var awsProfileResolvers = map[string]bool{
	"ProfileArgs":        true,
	"ProfileFlag":        true,
	"CallerIdentityArgs": true,
}

// stringSliceParams finds the parameters helpers thread resolved argv and
// profile flags down through, so an aws call referencing one was already
// targeted by its caller.
func stringSliceParams(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	for _, field := range fn.Type.Params.List {
		at, ok := field.Type.(*ast.ArrayType)
		if !ok {
			continue
		}
		if elt, ok := at.Elt.(*ast.Ident); !ok || elt.Name != "string" {
			continue
		}
		for _, name := range field.Names {
			names[name.Name] = true
		}
	}
	return names
}

// referencesAny treats a local built from a resolved parameter as carrying
// the same resolution.
func referencesAny(node ast.Node, names map[string]bool) bool {
	found := false
	ast.Inspect(node, func(inner ast.Node) bool {
		if id, ok := inner.(*ast.Ident); ok && names[id.Name] {
			found = true
		}
		return true
	})
	return found
}

func resolvesProfileName(name string) bool {
	return awsProfileResolvers[name] ||
		strings.HasSuffix(name, "ProfileArgs") ||
		strings.HasSuffix(name, "ProfileFlag")
}

// checkNoRawAWS refuses an aws CLI call in a function that never resolves
// the profile, since a bare call rides whatever AWS_PROFILE is ambient and
// can hit the wrong account. Unlike kubectl there is no single exec wrapper
// to route through, so the rule is scoped per function.
func checkNoRawAWS(ctx context.Context) error {
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
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		lines, err := scanGoForRawAWS(f, data)
		if err != nil {
			continue
		}
		for _, ln := range lines {
			offenders = append(offenders, fmt.Sprintf("%s:%d", f, ln))
			sparkwing.Info(ctx, "  aws without profile resolution: %s:%d", f, ln)
		}
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf(
			"aws CLI call(s) in a function that never resolves the profile -- append "+
				"aws.ProfileArgs(profile) (or aws.ProfileFlag) so the call targets an explicit "+
				"account, not whatever AWS_PROFILE is ambient:\n    %s",
			strings.Join(offenders, "\n    "),
		)
	}
	return nil
}

// scanGoForRawAWS scopes per function so a closure's aws call still sees a
// resolver reference in its enclosing body.
func scanGoForRawAWS(filename string, src []byte) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var offenders []int
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		var awsLines []int
		resolvesProfile := false
		sliceParams := stringSliceParams(fn)
		carries := func(node ast.Node) bool { return referencesAny(node, sliceParams) }
		ast.Inspect(fn, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, rhs := range assign.Rhs {
				if !carries(rhs) {
					continue
				}
				for _, lhs := range assign.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						sliceParams[id.Name] = true
					}
				}
			}
			return true
		})
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				if resolvesProfileName(fn.Sel.Name) {
					resolvesProfile = true
				}
			case *ast.Ident:
				if resolvesProfileName(fn.Name) {
					resolvesProfile = true
				}
			}
			if isExecCallee(call.Fun) {
				for _, arg := range call.Args {
					if carries(arg) {
						resolvesProfile = true
					}
				}
				for _, arg := range call.Args {
					lit, ok := arg.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					val, err := strconv.Unquote(lit.Value)
					if err != nil {
						continue
					}
					if awsWordRe.MatchString(val) {
						awsLines = append(awsLines, fset.Position(lit.Pos()).Line)
					}
				}
			}
			return true
		})
		if len(awsLines) > 0 && !resolvesProfile {
			offenders = append(offenders, awsLines...)
		}
	}
	sort.Ints(offenders)
	return offenders, nil
}
