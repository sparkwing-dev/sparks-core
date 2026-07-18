package coverage

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// coberturaRoot captures the attributes of a Cobertura report's root
// <coverage> element. lines-covered / lines-valid are exact integer
// counts and take precedence; line-rate (a rounded fraction in [0, 1])
// is the fallback used only when those counts are absent.
type coberturaRoot struct {
	XMLName      xml.Name `xml:"coverage"`
	LineRate     string   `xml:"line-rate,attr"`
	LinesCovered string   `xml:"lines-covered,attr"`
	LinesValid   string   `xml:"lines-valid,attr"`
}

// parseCobertura computes total line coverage from a Cobertura XML
// report. It prefers the exact lines-covered / lines-valid integer
// counts and falls back to the root line-rate attribute (a [0, 1]
// fraction scaled to a percentage) only when those counts are absent.
// line-rate is a value most producers round to a few decimal places, so
// the integer counts give a truer percentage for a gate near its floor.
func parseCobertura(data []byte) (float64, error) {
	var root coberturaRoot
	if err := xml.Unmarshal(data, &root); err != nil {
		return 0, fmt.Errorf("parse cobertura report: %w", err)
	}
	covered := strings.TrimSpace(root.LinesCovered)
	valid := strings.TrimSpace(root.LinesValid)
	if covered != "" && valid != "" {
		c, err := strconv.ParseInt(covered, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("malformed cobertura lines-covered %q: %w", covered, err)
		}
		v, err := strconv.ParseInt(valid, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("malformed cobertura lines-valid %q: %w", valid, err)
		}
		if c < 0 || v < 0 {
			return 0, fmt.Errorf("negative cobertura line count (covered %d, valid %d)", c, v)
		}
		if v == 0 {
			return 0, fmt.Errorf("cobertura report reports zero valid lines")
		}
		if c > v {
			return 0, fmt.Errorf("cobertura lines-covered (%d) exceeds lines-valid (%d)", c, v)
		}
		return 100 * float64(c) / float64(v), nil
	}
	lr := strings.TrimSpace(root.LineRate)
	if lr == "" {
		return 0, fmt.Errorf("cobertura report has neither lines-covered/lines-valid nor line-rate")
	}
	rate, err := strconv.ParseFloat(lr, 64)
	if err != nil {
		return 0, fmt.Errorf("malformed cobertura line-rate %q: %w", lr, err)
	}
	if rate < 0 || rate > 1 {
		return 0, fmt.Errorf("cobertura line-rate %v out of range [0,1]", rate)
	}
	return 100 * rate, nil
}
