// Package coverage parses Go coverprofile, lcov, and Cobertura XML reports
// into a single total-coverage percentage, and gates a pipeline on a floor.
// Parsing is pure: no host tools and no network.
package coverage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

type Report struct {
	// Format is "go", "lcov", or "cobertura", defaulting to "go".
	Format string
	// Path is absolute, or relative to sparkwing.WorkDir() -- a compiled
	// pipeline binary does not run with the repo root as its cwd.
	Path string
}

// Supported report formats accepted in Report.Format.
const (
	FormatGo        = "go"
	FormatLCOV      = "lcov"
	FormatCobertura = "cobertura"
)

func (r Report) format() string {
	f := strings.ToLower(strings.TrimSpace(r.Format))
	if f == "" {
		return FormatGo
	}
	return f
}

// Total returns the report's total coverage as a percentage in [0, 100].
func Total(_ context.Context, r Report) (float64, error) {
	data, err := os.ReadFile(resolvePath(r.Path))
	if err != nil {
		return 0, fmt.Errorf("read coverage report %q: %w", r.Path, err)
	}
	switch f := r.format(); f {
	case FormatGo:
		return parseGoProfile(data)
	case FormatLCOV:
		return parseLCOV(data)
	case FormatCobertura:
		return parseCobertura(data)
	default:
		return 0, fmt.Errorf("unknown coverage format %q (want %s, %s, or %s)",
			r.Format, FormatGo, FormatLCOV, FormatCobertura)
	}
}

// GateAtLeast returns a Verify-shaped check that fails when the report's
// total coverage is below floor percent.
func GateAtLeast(floor float64, r Report) func(context.Context) error {
	return func(ctx context.Context) error {
		return step.Run(ctx, "coverage gate", func(ctx context.Context) error {
			total, err := Total(ctx, r)
			if err != nil {
				return err
			}
			sparkwing.Info(ctx, "  total coverage: %.1f%% (floor %.1f%%)", total, floor)
			if total < floor {
				return fmt.Errorf("coverage %.1f%% is below the required %.1f%% floor (%s report %s)",
					total, floor, r.format(), r.Path)
			}
			return nil
		})
	}
}

func resolvePath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	root := sparkwing.WorkDir()
	if root == "" {
		return p
	}
	return filepath.Join(root, p)
}
