package release

import (
	"strings"
	"testing"
)

const sampleChangelog = `# Changelog

All notable changes.

## [Unreleased]

### Added
- work in progress

## [1.2.0] - 2024-03-01

### Added
- shiny feature
- another thing

### Fixed
- a bug

## [1.1.0] - 2024-01-15

### Changed
- adjusted defaults

## [1.0.0] - 2023-12-01

- initial release
`

func TestParseChangelog_TopReleasedSectionSkipsUnreleased(t *testing.T) {
	notes, version, err := parseChangelog(sampleChangelog, "")
	if err != nil {
		t.Fatalf("parseChangelog: %v", err)
	}
	if version != "1.2.0" {
		t.Errorf("version = %q, want 1.2.0", version)
	}
	if !strings.Contains(notes, "shiny feature") || !strings.Contains(notes, "a bug") {
		t.Errorf("notes missing expected content:\n%s", notes)
	}
	if strings.Contains(notes, "work in progress") {
		t.Errorf("notes leaked the Unreleased section:\n%s", notes)
	}
	if strings.Contains(notes, "adjusted defaults") {
		t.Errorf("notes leaked the next section:\n%s", notes)
	}
	if strings.Contains(notes, "## [1.1.0]") {
		t.Errorf("notes should stop before the next heading:\n%s", notes)
	}
}

func TestParseChangelog_SpecificVersion(t *testing.T) {
	notes, version, err := parseChangelog(sampleChangelog, "1.1.0")
	if err != nil {
		t.Fatalf("parseChangelog: %v", err)
	}
	if version != "1.1.0" {
		t.Errorf("version = %q, want 1.1.0", version)
	}
	if !strings.Contains(notes, "adjusted defaults") {
		t.Errorf("notes missing expected content:\n%s", notes)
	}
}

func TestParseChangelog_VersionWithLeadingVMatches(t *testing.T) {
	_, version, err := parseChangelog(sampleChangelog, "v1.2.0")
	if err != nil {
		t.Fatalf("parseChangelog: %v", err)
	}
	if version != "1.2.0" {
		t.Errorf("version = %q, want 1.2.0", version)
	}
}

func TestParseChangelog_LastSectionRunsToEnd(t *testing.T) {
	notes, _, err := parseChangelog(sampleChangelog, "1.0.0")
	if err != nil {
		t.Fatalf("parseChangelog: %v", err)
	}
	if !strings.Contains(notes, "initial release") {
		t.Errorf("notes missing expected content:\n%s", notes)
	}
}

func TestParseChangelog_MissingVersion(t *testing.T) {
	_, _, err := parseChangelog(sampleChangelog, "9.9.9")
	if err == nil {
		t.Fatal("expected error for a version not in the changelog")
	}
}

func TestParseChangelog_OnlyUnreleased(t *testing.T) {
	_, _, err := parseChangelog("# Changelog\n\n## [Unreleased]\n\n- pending\n", "")
	if err == nil {
		t.Fatal("expected error when only an Unreleased section exists")
	}
}

func TestParseChangelog_PlainHeadingForm(t *testing.T) {
	cl := "## v2.0.0\n\n- released\n\n## v1.0.0\n\n- old\n"
	notes, version, err := parseChangelog(cl, "")
	if err != nil {
		t.Fatalf("parseChangelog: %v", err)
	}
	if version != "v2.0.0" {
		t.Errorf("version = %q, want v2.0.0", version)
	}
	if !strings.Contains(notes, "released") || strings.Contains(notes, "old") {
		t.Errorf("wrong section body:\n%s", notes)
	}
}

func TestParseChangelog_NoSections(t *testing.T) {
	_, _, err := parseChangelog("# Changelog\n\nnothing structured here\n", "")
	if err == nil {
		t.Fatal("expected error when no '## ' sections exist")
	}
}
