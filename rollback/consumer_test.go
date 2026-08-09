package rollback_test

import (
	"context"
	"testing"

	"github.com/sparkwing-dev/sparks-core/rollback"
)

func TestPublishedDependencyContractCompiles(t *testing.T) {
	err := rollback.Run(context.Background(), rollback.Config{Local: true})
	if err == nil {
		t.Fatal("rollback with no deployments must fail")
	}
}
