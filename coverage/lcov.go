package coverage

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// parseLCOV sums the per-record LH lines over the LF lines.
func parseLCOV(data []byte) (float64, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var found, hit int64
	var sawLF bool
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "LF:"):
			n, err := parseLCOVCount(line, "LF:")
			if err != nil {
				return 0, err
			}
			found += n
			sawLF = true
		case strings.HasPrefix(line, "LH:"):
			n, err := parseLCOVCount(line, "LH:")
			if err != nil {
				return 0, err
			}
			hit += n
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("read lcov report: %w", err)
	}
	if !sawLF {
		return 0, fmt.Errorf("lcov report has no LF (lines-found) records")
	}
	if found == 0 {
		return 0, fmt.Errorf("lcov report reports zero found lines")
	}
	if hit > found {
		return 0, fmt.Errorf("lcov report has more hit lines (%d) than found lines (%d)", hit, found)
	}
	return 100 * float64(hit) / float64(found), nil
}

func parseLCOVCount(line, prefix string) (int64, error) {
	v := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed lcov count %q: %w", line, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative lcov count: %q", line)
	}
	return n, nil
}
