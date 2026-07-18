package kube

import (
	"context"
	"testing"
)

func TestApply_RequiresPath(t *testing.T) {
	clearKubeEnv(t)
	if err := Apply(context.Background(), ApplyConfig{Namespace: "prod"}); err == nil {
		t.Fatal("expected an error when no path is set")
	}
}

func TestApply_DryRunSkipsExec(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := Apply(context.Background(), ApplyConfig{
		Paths:      []string{"k8s/app.yaml"},
		Namespace:  "prod",
		ServerSide: true,
		Wait:       []string{"deploy/myapp"},
		Context:    "team-staging",
	})
	if err != nil {
		t.Fatalf("dry-run Apply should succeed without a cluster, got: %v", err)
	}
}

func TestSetImage_DryRunSkipsExec(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := SetImage(context.Background(), SetImageConfig{
		Deployment: "deploy/myapp",
		Container:  "app",
		Image:      "registry/app:abc123",
		Namespace:  "prod",
		Context:    "team-staging",
	})
	if err != nil {
		t.Fatalf("dry-run SetImage should succeed without a cluster, got: %v", err)
	}
}

func TestRolloutUndo_DryRunSkipsExec(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := RolloutUndo(context.Background(), RolloutUndoConfig{
		Deployments: []string{"deploy/myapp"},
		Namespace:   "prod",
		Context:     "team-staging",
	})
	if err != nil {
		t.Fatalf("dry-run RolloutUndo should succeed without a cluster, got: %v", err)
	}
}
