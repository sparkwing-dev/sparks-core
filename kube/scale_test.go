package kube

import (
	"context"
	"reflect"
	"testing"
)

func TestScaleArgs(t *testing.T) {
	got := scaleArgs("prod", "deploy/myapp-canary", 3)
	want := []string{"scale", "deploy/myapp-canary", "--replicas=3", "-n", "prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaleArgs = %v, want %v", got, want)
	}
}

func TestScaleArgs_ZeroReplicas(t *testing.T) {
	got := scaleArgs("default", "deploy/myapp-canary", 0)
	want := []string{"scale", "deploy/myapp-canary", "--replicas=0", "-n", "default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scaleArgs = %v, want %v", got, want)
	}
}

func TestRolloutStatusArgs(t *testing.T) {
	got := rolloutStatusArgs("prod", "deploy/myapp-canary", "180s")
	want := []string{"rollout", "status", "deploy/myapp-canary", "-n", "prod", "--timeout=180s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rolloutStatusArgs = %v, want %v", got, want)
	}
}

func TestScale_RequiresDeployment(t *testing.T) {
	clearKubeEnv(t)
	if err := Scale(context.Background(), ScaleConfig{Replicas: 2}); err == nil {
		t.Fatal("expected an error for an empty Deployment")
	}
}

func TestScale_RejectsNegativeReplicas(t *testing.T) {
	clearKubeEnv(t)
	if err := Scale(context.Background(), ScaleConfig{Deployment: "deploy/x", Replicas: -1}); err == nil {
		t.Fatal("expected an error for a negative replica count")
	}
}

func TestScale_DryRunSkipsExec(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := Scale(context.Background(), ScaleConfig{
		Deployment: "deploy/myapp-canary",
		Replicas:   0,
		Namespace:  "prod",
		Context:    "team-staging",
	})
	if err != nil {
		t.Fatalf("dry-run Scale should succeed without a cluster, got: %v", err)
	}
}

func TestScale_DefaultsNamespaceAndTimeout(t *testing.T) {
	cfg := ScaleConfig{Deployment: "deploy/x"}
	cfg.defaults()
	if cfg.Namespace != "default" {
		t.Errorf("Namespace default = %q, want default", cfg.Namespace)
	}
	if cfg.Timeout != "180s" {
		t.Errorf("Timeout default = %q, want 180s", cfg.Timeout)
	}
}
