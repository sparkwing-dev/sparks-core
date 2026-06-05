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

// kubectlChokepoint is the one file allowed to shell out to kubectl.
// Every other kubectl call must route through the helpers it defines so
// the --context is resolved explicitly (kube.ResolveContext) and fails
// closed -- never silently the current kubeconfig context.
const kubectlChokepoint = "kube/context.go"

// execCallees are the function names that actually run a command.
// A "kubectl" string is only a real invocation when passed to one of
// these -- which keeps the scan off the same word in a log line, error
// message, or comment.
var execCallees = map[string]bool{
	"Exec":           true, // step.Exec / sparkwing.Exec (arg-vector)
	"Bash":           true, // sparkwing.Bash (shell line)
	"Sh":             true, // step.Sh
	"Command":        true, // os/exec.Command
	"CommandContext": true,
}

var kubectlWordRe = regexp.MustCompile(`\bkubectl\b`)

// checkNoRawKubectl fails the push if any tracked Go file shells out to
// kubectl directly outside the chokepoint. This is the static guard for
// the "deploy/rollback silently hit the current (wrong) cluster" class
// of bug: it forces every kubectl call through one context-aware helper.
func checkNoRawKubectl(ctx context.Context) error {
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
		if f == kubectlChokepoint {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		lines, err := scanGoForRawKubectl(f, data)
		if err != nil {
			// Unparseable Go is gofmt/vet's problem, not ours.
			continue
		}
		for _, ln := range lines {
			offenders = append(offenders, fmt.Sprintf("%s:%d", f, ln))
			sparkwing.Info(ctx, "  raw kubectl: %s:%d", f, ln)
		}
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		return fmt.Errorf(
			"raw kubectl call(s) outside %s -- route through the kube blocks so --context is "+
				"always explicit (a context-less kubectl targets the current kubeconfig context, "+
				"which may be the wrong cluster):\n    %s",
			kubectlChokepoint, strings.Join(offenders, "\n    "),
		)
	}
	return nil
}

// scanGoForRawKubectl parses one Go source file and returns the line
// numbers of any kubectl command passed to an exec helper. Pure and
// side-effect free for unit testing.
func scanGoForRawKubectl(filename string, src []byte) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return nil, err
	}
	var lines []int
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isExecCallee(call.Fun) {
			return true
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
			if kubectlWordRe.MatchString(val) {
				lines = append(lines, fset.Position(lit.Pos()).Line)
			}
		}
		return true
	})
	return lines, nil
}

func isExecCallee(fun ast.Expr) bool {
	switch e := fun.(type) {
	case *ast.SelectorExpr:
		return execCallees[e.Sel.Name]
	case *ast.Ident:
		return execCallees[e.Name]
	}
	return false
}
