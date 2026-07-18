package coverage

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// parseGoProfile computes the statement-weighted total coverage of a Go
// coverprofile. Each data line is
//
//	name.go:startLine.col,endLine.col numStmts count
//
// and the total is the sum of numStmts over blocks with count > 0,
// divided by the sum of all numStmts -- the same figure `go tool cover
// -func` reports as `total:`. A leading `mode:` line is ignored.
func parseGoProfile(data []byte) (float64, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var covered, total int64
	var blocks int
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return 0, fmt.Errorf("malformed Go coverprofile line: %q", line)
		}
		numStmts, err := strconv.ParseInt(fields[len(fields)-2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("malformed statement count in %q: %w", line, err)
		}
		count, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("malformed hit count in %q: %w", line, err)
		}
		if numStmts < 0 || count < 0 {
			return 0, fmt.Errorf("negative count in Go coverprofile line: %q", line)
		}
		blocks++
		total += numStmts
		if count > 0 {
			covered += numStmts
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("read Go coverprofile: %w", err)
	}
	if blocks == 0 {
		return 0, fmt.Errorf("Go coverprofile has no coverage blocks")
	}
	if total == 0 {
		return 0, fmt.Errorf("Go coverprofile reports zero total statements")
	}
	return 100 * float64(covered) / float64(total), nil
}
