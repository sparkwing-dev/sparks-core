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
// function that shells aws must reference one of them.
var awsProfileResolvers = map[string]bool{
	"ProfileArgs": true,
	"ProfileFlag": true,
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
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && awsProfileResolvers[sel.Sel.Name] {
				resolvesProfile = true
			}
			if isExecCallee(call.Fun) {
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
