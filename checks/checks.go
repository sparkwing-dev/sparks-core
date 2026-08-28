// Package checks holds canned pre-commit and pre-push checks for Go
// repositories, each shaped to slot into a sparkwing Step or Plan node.
package checks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

// GoFmt checks that every tracked Go file is formatted canonically.
func GoFmt(ctx context.Context) error {
	return step.Run(ctx, "go fmt", func(ctx context.Context) error {
		out, err := sparkwing.Bash(ctx, "git ls-files '*.go' | xargs gofmt -l 2>/dev/null").String()
		if err != nil {
			return err
		}
		if out == "" {
			return nil
		}
		for _, f := range strings.Split(out, "\n") {
			if f != "" {
				sparkwing.Info(ctx, "  needs formatting: %s", f)
			}
		}
		return errors.New("files need formatting; run gofmt -w")
	})
}

// GoVet runs go vet on pkgs, defaulting to ./..., rewriting a pattern that
// points into a nested module as `go -C <dir>`.
func GoVet(ctx context.Context, pkgs ...string) error {
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}
	return step.Run(ctx, "go vet", func(ctx context.Context) error {
		dir, resolved := resolveModuleDir(pkgs)
		args := append([]string{"vet"}, resolved...)
		if dir != "" {
			args = append([]string{"-C", dir, "vet"}, resolved...)
		}
		return step.Exec(ctx, "go", args...)
	})
}

// GoTestShort runs go test -short on pkgs, defaulting to ./....
func GoTestShort(ctx context.Context, pkgs ...string) error {
	return runGoTest(ctx, "tests (short)", []string{"-short"}, pkgs)
}

// GoTest runs go test on pkgs, defaulting to ./....
func GoTest(ctx context.Context, pkgs ...string) error {
	return runGoTest(ctx, "tests", nil, pkgs)
}

func runGoTest(ctx context.Context, label string, flags, pkgs []string) error {
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}
	return step.Run(ctx, label, func(ctx context.Context) error {
		dir, resolved := resolveModuleDir(pkgs)
		var args []string
		if dir != "" {
			args = append(args, "-C", dir, "test")
		} else {
			args = append(args, "test")
		}
		args = append(args, flags...)
		args = append(args, resolved...)
		return step.Exec(ctx, "go", args...)
	})
}

// resolveModuleDir rewrites ["./myapp/..."] to ("myapp", ["./..."]) when
// myapp holds its own go.mod. It stats relative to sparkwing.WorkDir()
// because the compiled pipeline binary runs out of the build cache, whose
// cwd is not the repo root.
func resolveModuleDir(pkgs []string) (string, []string) {
	if len(pkgs) != 1 {
		return "", pkgs
	}
	pkg := pkgs[0]
	if !strings.HasPrefix(pkg, "./") {
		return "", pkgs
	}
	rest := pkg[2:]
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return "", pkgs
	}
	dir := rest[:idx]
	suffix := rest[idx:]

	root := sparkwing.WorkDir()
	if root == "" {
		root = "."
	}
	if _, err := os.Stat(filepath.Join(root, dir, "go.mod")); err != nil {
		return "", pkgs
	}
	return dir, []string{"./" + strings.TrimPrefix(suffix, "/")}
}

// TrailingNewlines checks that every tracked text file ends with a newline.
func TrailingNewlines(ctx context.Context) error {
	return step.Run(ctx, "trailing newlines", func(ctx context.Context) error {
		root := sparkwing.WorkDir()
		if root == "" {
			root = "."
		}
		bad := findMissingNewlines(ctx, root)
		if len(bad) == 0 {
			return nil
		}
		for _, f := range bad {
			sparkwing.Info(ctx, "  missing trailing newline: %s", f)
		}
		return fmt.Errorf("files missing trailing newlines")
	})
}

func findMissingNewlines(ctx context.Context, root string) []string {
	files, err := sparkwing.Bash(ctx, "git ls-files").Lines()
	if err != nil {
		return nil
	}
	var bad []string
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp",
			".woff", ".woff2", ".ttf", ".eot",
			".pdf", ".zip", ".gz", ".tar", ".bin":
			continue
		}
		path := filepath.Join(root, f)
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		check := data
		if len(check) > 512 {
			check = check[:512]
		}
		isBinary := false
		for _, b := range check {
			if b == 0 {
				isBinary = true
				break
			}
		}
		if isBinary {
			continue
		}
		if data[len(data)-1] != '\n' {
			bad = append(bad, f)
		}
	}
	return bad
}
