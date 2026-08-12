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

// awsProfileResolvers are the helpers that make an aws CLI call target an
// explicit account/profile (or correctly no profile under IRSA). A
// function that shells aws must reference one of them, directly or
// through a local wrapper: resolvesProfileName also accepts any name
// ending in ProfileArgs or ProfileFlag, which is how ecs and lambda
// build their argv (regionProfileArgs).
var awsProfileResolvers = map[string]bool{
	"ProfileArgs": true,
	"ProfileFlag": true,
	// CallerIdentityArgs appends ProfileArgs itself, and is what a
	// caller runs to confirm the account before a destructive call.
	"CallerIdentityArgs": true,
}

// resolvesProfileName reports whether a called function resolves the aws
// profile, either by being one of awsProfileResolvers or by wrapping one
// under a name that carries the same suffix.
func resolvesProfileName(name string) bool {
	return awsProfileResolvers[name] ||
		strings.HasSuffix(name, "ProfileArgs") ||
		strings.HasSuffix(name, "ProfileFlag")
}

// checkNoRawAWS fails the push if any function shells out to the aws CLI
// without also resolving the AWS profile (aws.ProfileArgs / ProfileFlag).
// A bare aws call rides ambient credentials -- whatever AWS_PROFILE
// happens to be set, or the default -- which can hit the wrong account.
// Unlike kubectl there's no single exec wrapper; the safe pattern is to
// append the profile flags, so the rule is scoped per function: if you
// run aws here, resolve the profile here.
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

// scanGoForRawAWS returns the line numbers of aws CLI calls that live in
// a function which never references a profile resolver. Per-function
// scope so a closure's aws call still sees a ProfileArgs reference in its
// enclosing function body. Pure for unit testing.
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
		// Helpers take the resolved argv, or the resolved profile flags,
		// as a []string parameter and thread it down (ecs's rp, lambda's
		// args). The resolution happened in the caller, so an aws call
		// whose argv references such a parameter is already targeted.
		sliceParams := map[string]bool{}
		for _, field := range fn.Type.Params.List {
			at, ok := field.Type.(*ast.ArrayType)
			if !ok {
				continue
			}
			if elt, ok := at.Elt.(*ast.Ident); !ok || elt.Name != "string" {
				continue
			}
			for _, name := range field.Names {
				sliceParams[name.Name] = true
			}
		}
		// A local built from one of those parameters carries the same
		// resolution: ecs does args := describeServicesArgs(..., rp)
		// and then execs args.
		carries := func(node ast.Node) bool {
			found := false
			ast.Inspect(node, func(inner ast.Node) bool {
				if id, ok := inner.(*ast.Ident); ok && sliceParams[id.Name] {
					found = true
				}
				return true
			})
			return found
		}
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
