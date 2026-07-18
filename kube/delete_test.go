package kube

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDeletePathArgs_IgnoreNotFound(t *testing.T) {
	got := deletePathArgs("prod", "k8s/canary.yaml", true)
	want := []string{"delete", "-n", "prod", "-f", "k8s/canary.yaml", "--ignore-not-found"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deletePathArgs = %v, want %v", got, want)
	}
}

func TestDeletePathArgs_WithoutIgnoreNotFound(t *testing.T) {
	got := deletePathArgs("default", "k8s/canary.yaml", false)
	want := []string{"delete", "-n", "default", "-f", "k8s/canary.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deletePathArgs = %v, want %v", got, want)
	}
}

func TestDeleteResourceArgs_IgnoreNotFound(t *testing.T) {
	got := deleteResourceArgs("prod", "deploy/myapp-canary", true)
	want := []string{"delete", "deploy/myapp-canary", "-n", "prod", "--ignore-not-found"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deleteResourceArgs = %v, want %v", got, want)
	}
}

func TestDelete_RequiresPathOrResource(t *testing.T) {
	clearKubeEnv(t)
	err := Delete(context.Background(), DeleteConfig{Namespace: "prod"})
	if err == nil {
		t.Fatal("expected an error when neither Paths nor Resources is set")
	}
}

func TestDelete_FailsClosedWithoutContext(t *testing.T) {
	clearKubeEnv(t)
	err := Delete(context.Background(), DeleteConfig{Resources: []string{"deploy/myapp-canary"}})
	if err == nil {
		t.Fatal("expected a fail-closed context error with no context configured")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Fatalf("error should mention the missing context, got: %v", err)
	}
}

func TestDelete_DryRunSkipsExecWithoutContext(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := Delete(context.Background(), DeleteConfig{
		Paths:          []string{"k8s/canary.yaml"},
		Resources:      []string{"deploy/myapp-canary", "service/myapp-canary"},
		Namespace:      "prod",
		IgnoreNotFound: true,
	})
	if err != nil {
		t.Fatalf("dry-run Delete should succeed without a cluster, got: %v", err)
	}
}

func TestDelete_DryRunWithExplicitContext(t *testing.T) {
	clearKubeEnv(t)
	t.Setenv(dryRunEnv, "1")
	err := Delete(context.Background(), DeleteConfig{
		Resources: []string{"deploy/myapp-canary"},
		Context:   "team-staging",
	})
	if err != nil {
		t.Fatalf("dry-run Delete with an explicit context should succeed, got: %v", err)
	}
}
