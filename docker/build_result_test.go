package docker

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/sparkwing-dev/sparkwing/sparkwing/docker"
)

func TestBuildAndPushResultDryRunPreservesOldContract(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	cfg := BuildConfig{Image: "app", Dockerfile: "app/Dockerfile", Context: "app", Platform: "linux/arm64", Registries: []string{"example.invalid"}, Tags: sdk.ImageTag{Commit: "abc", Content: "files", Branch: "main"}, CacheFrom: []string{"type=registry,ref=cache"}, CacheTo: []string{"type=local,dest=cache"}}
	result, err := BuildAndPushResult(context.Background(), cfg)
	if err != nil || len(result.Digests) != 0 {
		t.Fatalf("dry-run result = %+v, %v", result, err)
	}
	if err := BuildAndPush(context.Background(), cfg); err != nil {
		t.Fatalf("old API dry-run = %v", err)
	}
}

func TestPushedManifestDigestDoesNotInferContent(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	footer := "tag: digest: " + digest + " size: 123\n"
	for _, tc := range []struct{ output, want string }{{footer, digest}, {footer + footer, digest}, {footer + strings.ReplaceAll(footer, "aaaa", "bbbb"), ""}, {strings.ReplaceAll(footer, "tag:", "tag-evil:"), ""}, {digest, ""}, {"tag: digest: sha256:short size: 1\n", ""}} {
		if got := pushedManifestDigest(tc.output, "tag"); got != tc.want {
			t.Fatalf("digest = %q, want %q", got, tc.want)
		}
	}
}
