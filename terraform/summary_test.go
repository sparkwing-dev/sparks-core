package terraform

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseChangeSummary_ParsesAddChangeDestroy(t *testing.T) {
	got := ParseChangeSummary(readFixture(t, "plan_changes.txt"))
	if got.Adds != 2 || got.Changes != 1 || got.Destroys != 0 {
		t.Fatalf("counts = (%d,%d,%d), want (2,1,0)", got.Adds, got.Changes, got.Destroys)
	}
	if got.Summary != "Plan: 2 to add, 1 to change, 0 to destroy." {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestParseChangeSummary_DestroyOnly(t *testing.T) {
	got := ParseChangeSummary(readFixture(t, "plan_destroy.txt"))
	if got.Adds != 0 || got.Changes != 0 || got.Destroys != 3 {
		t.Fatalf("counts = (%d,%d,%d), want (0,0,3)", got.Adds, got.Changes, got.Destroys)
	}
}

func TestParseChangeSummary_MixedLargeCounts(t *testing.T) {
	got := ParseChangeSummary(readFixture(t, "plan_mixed.txt"))
	if got.Adds != 10 || got.Changes != 4 || got.Destroys != 2 {
		t.Fatalf("counts = (%d,%d,%d), want (10,4,2)", got.Adds, got.Changes, got.Destroys)
	}
}

func TestParseChangeSummary_NoChanges(t *testing.T) {
	got := ParseChangeSummary(readFixture(t, "plan_nochanges.txt"))
	if got.Adds != 0 || got.Changes != 0 || got.Destroys != 0 {
		t.Fatalf("counts = (%d,%d,%d), want all zero", got.Adds, got.Changes, got.Destroys)
	}
	if got.Summary != "No changes. Your infrastructure matches the configuration." {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestParseChangeSummary_UnrecognizedIsZeroAndEmpty(t *testing.T) {
	got := ParseChangeSummary("Error: something went wrong\nnot a plan\n")
	if got.Adds != 0 || got.Changes != 0 || got.Destroys != 0 || got.Summary != "" {
		t.Fatalf("got %+v, want zero-value", got)
	}
}
