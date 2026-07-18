// Package coverage parses code-coverage reports and gates a pipeline on
// a total-coverage floor.
//
// Three report formats are understood, each reduced to a single
// total-line/statement-coverage percentage:
//
//   - Go coverprofile (the file `go test -coverprofile=cover.out`
//     writes): the statement-weighted total, matching the `total:`
//     figure `go tool cover -func` prints.
//   - lcov tracefiles (`.info`): the sum of hit lines (LH) over found
//     lines (LF) across every record.
//   - Cobertura XML: the root <coverage> element's line-rate, scaled to
//     a percentage.
//
// Parsing is pure: no host tools are shelled out to and no network is
// touched, so the same report drives a gate for a Go, Node, Python, or
// Rust suite that emitted one of these formats.
//
// The unit of work is Verify-shaped. GateAtLeast returns a
// func(context.Context) error that reads the report, logs the total,
// and fails with a rich error when it falls below the floor, so it
// plugs directly into a sparkwing Job.Verify:
//
//	sw.Job(plan, "test", runTests).
//	    Verify(coverage.GateAtLeast(80, coverage.Report{Format: "go", Path: "cover.out"}))
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

// Report identifies a coverage report to parse: its on-disk location
// and which format's parser to apply.
type Report struct {
	// Format selects the parser: "go" (Go coverprofile), "lcov", or
	// "cobertura". An empty value defaults to "go".
	Format string
	// Path is the report file, absolute or relative to the sparkwing
	// work directory (the repo root). A relative path is resolved
	// against sparkwing.WorkDir() because a compiled pipeline binary
	// does not run with the repo root as its cwd.
	Path string
}

// Supported report formats accepted in Report.Format.
const (
	FormatGo        = "go"
	FormatLCOV      = "lcov"
	FormatCobertura = "cobertura"
)

// format returns the normalized, lower-cased format, defaulting an
// empty value to FormatGo.
func (r Report) format() string {
	f := strings.ToLower(strings.TrimSpace(r.Format))
	if f == "" {
		return FormatGo
	}
	return f
}

// Total reads the report and returns its total coverage as a percentage
// in [0, 100]. It errors on an unknown format, an unreadable file, or a
// report that does not parse into a coverage figure.
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
// total coverage is below min percent. On success it logs the measured
// total; on a shortfall it returns an error naming the total, the floor,
// and the report.
func GateAtLeast(min float64, r Report) func(context.Context) error {
	return func(ctx context.Context) error {
		return step.Run(ctx, "coverage gate", func(ctx context.Context) error {
			total, err := Total(ctx, r)
			if err != nil {
				return err
			}
			sparkwing.Info(ctx, "  total coverage: %.1f%% (floor %.1f%%)", total, min)
			if total < min {
				return fmt.Errorf("coverage %.1f%% is below the required %.1f%% floor (%s report %s)",
					total, min, r.format(), r.Path)
			}
			return nil
		})
	}
}

// resolvePath makes a relative report path absolute against the
// sparkwing work directory; an absolute path is returned unchanged.
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
