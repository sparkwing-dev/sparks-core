package contentkey

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func TestCacheResolversPreserveFailures(t *testing.T) {
	previous := sparkwing.WorkDir()
	sparkwing.SetWorkDir(t.TempDir())
	t.Cleanup(func() { sparkwing.SetWorkDir(previous) })
	resolvers := map[string]any{
		"paths":          OfPaths("*.go"),
		"salted paths":   Salted("example", "*.go"),
		"package":        OfGoPackage("./example"),
		"salted package": SaltedGoPackage("example", "./example"),
	}
	for name, candidate := range resolvers {
		t.Run(name, func(t *testing.T) {
			resolver, ok := candidate.(sparkwing.CacheKeyFn)
			if !ok {
				t.Fatalf("resolver type = %T, want sparkwing.CacheKeyFn with an error result", candidate)
			}
			t.Run("cancelled", func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				key, err := resolver(ctx)
				if key != "" || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled resolution = %q, %v; want empty key and cancellation cause", key, err)
				}
			})
			t.Run("missing tool", func(t *testing.T) {
				t.Setenv("PATH", t.TempDir())
				key, err := resolver(t.Context())
				var toolError *exec.Error
				if key != "" || !errors.As(err, &toolError) {
					t.Fatalf("resolution without tools = %q, %v; want empty key and executable cause", key, err)
				}
			})
		})
	}
}
