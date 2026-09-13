package jobs

import "testing"

func TestMarkdownlintCommandIsPinned(t *testing.T) {
	const want = "npx --yes markdownlint-cli2@0.23.2"
	if markdownlintCommand != want {
		t.Fatalf("markdownlint command = %q, want exactly %q", markdownlintCommand, want)
	}
}
