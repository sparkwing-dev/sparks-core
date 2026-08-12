package kube

import "testing"

func clearKubeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("SPARKWING_KUBE_CONTEXT", "")
	t.Setenv("SPARKWING_KIND_CLUSTER", "")
	t.Setenv("SPARKWING_KUBE_ALLOW_CURRENT", "")
}

func TestResolveContext_ExplicitWins(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv("SPARKWING_KUBE_CONTEXT", "env-ctx")
	got, err := ResolveContext("explicit-ctx")
	if err != nil || got != "explicit-ctx" {
		t.Fatalf("ResolveContext = (%q, %v), want (explicit-ctx, nil)", got, err)
	}
}

func TestResolveContext_InClusterReturnsEmpty(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("SPARKWING_KUBE_CONTEXT", "should-be-ignored")
	got, err := ResolveContext("")
	if err != nil || got != "" {
		t.Fatalf("ResolveContext in-cluster = (%q, %v), want (\"\", nil)", got, err)
	}
}

// The environment cannot name a cluster any more, so a variable left
// over from another project no longer decides where kubectl points.
func TestResolveContext_EnvCannotNameACluster(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv("SPARKWING_KUBE_CONTEXT", "team-staging")
	t.Setenv("SPARKWING_KIND_CLUSTER", "sparktest")
	t.Setenv("SPARKWING_KUBE_ALLOW_CURRENT", "1")
	if _, err := ResolveContext(""); err == nil {
		t.Fatal("ResolveContext resolved a cluster from the environment, want an error")
	}
}

func TestResolveContext_FailsClosed(t *testing.T) {
	clearKubeEnv(t)
	got, err := ResolveContext("")
	if err == nil {
		t.Fatalf("ResolveContext with nothing set = (%q, nil), want an error (fail closed)", got)
	}
}
