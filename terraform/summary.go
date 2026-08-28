package terraform

import (
	"regexp"
	"strconv"
	"strings"
)

type ChangeSummary struct {
	Adds     int
	Changes  int
	Destroys int
	// Summary is the trimmed line the counts came from, empty when none
	// was recognized.
	Summary string
}

var planLineRE = regexp.MustCompile(`Plan:\s+(\d+)\s+to add,\s+(\d+)\s+to change,\s+(\d+)\s+to destroy`)

// ParseChangeSummary reads the "Plan: ..." line or the "No changes."
// message, leaving zero counts when neither is present. It expects
// -no-color output and does no ANSI stripping.
func ParseChangeSummary(stdout string) ChangeSummary {
	var cs ChangeSummary
	for _, line := range strings.Split(stdout, "\n") {
		if m := planLineRE.FindStringSubmatch(line); m != nil {
			cs.Adds = atoi(m[1])
			cs.Changes = atoi(m[2])
			cs.Destroys = atoi(m[3])
			cs.Summary = strings.TrimSpace(line)
			return cs
		}
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "No changes.") {
			cs.Summary = strings.TrimSpace(line)
			return cs
		}
	}
	return cs
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
