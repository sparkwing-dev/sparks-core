package terraform

import (
	"regexp"
	"strconv"
	"strings"
)

// ChangeSummary is the parsed result of a `terraform plan` change line.
type ChangeSummary struct {
	Adds     int
	Changes  int
	Destroys int
	// Summary is the human-readable line the counts came from (the
	// "Plan: ..." line, or the "No changes." line), trimmed. Empty when
	// no recognizable summary was found.
	Summary string
}

var planLineRE = regexp.MustCompile(`Plan:\s+(\d+)\s+to add,\s+(\d+)\s+to change,\s+(\d+)\s+to destroy`)

// ParseChangeSummary extracts the add/change/destroy counts from
// `terraform plan` stdout. It recognizes the "Plan: N to add, N to
// change, N to destroy." line and the "No changes." message (both worded
// as counts of zero). When neither is present the counts are zero and
// Summary is empty.
//
// Parse against -no-color output; the Plan block passes -no-color so no
// ANSI stripping is needed here.
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
