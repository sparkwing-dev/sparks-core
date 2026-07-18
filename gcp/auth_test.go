package gcp

import (
	"context"
	"reflect"
	"testing"
)

func TestConfigureDockerArgs(t *testing.T) {
	got := configureDockerArgs("us-west1-docker.pkg.dev")
	want := []string{"auth", "configure-docker", "us-west1-docker.pkg.dev", "--quiet"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configureDockerArgs = %v, want %v", got, want)
	}
}

func TestGetCredentialsArgs_FullConfig(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "")
	got := getCredentialsArgs(GKEConfig{Cluster: "prod", Location: "us-west1", Project: "my-proj"})
	want := []string{
		"container", "clusters", "get-credentials", "prod",
		"--location", "us-west1",
		"--project", "my-proj",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getCredentialsArgs = %v, want %v", got, want)
	}
}

func TestGetCredentialsArgs_NoProjectNoLocation(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "")
	got := getCredentialsArgs(GKEConfig{Cluster: "dev"})
	want := []string{"container", "clusters", "get-credentials", "dev"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getCredentialsArgs = %v, want %v", got, want)
	}
}

func TestGetCredentialsArgs_Impersonation(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "deployer@proj.iam.gserviceaccount.com")
	got := getCredentialsArgs(GKEConfig{Cluster: "prod", Location: "us-west1", Project: "my-proj"})
	want := []string{
		"container", "clusters", "get-credentials", "prod",
		"--location", "us-west1",
		"--project", "my-proj",
		"--impersonate-service-account", "deployer@proj.iam.gserviceaccount.com",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getCredentialsArgs = %v, want %v", got, want)
	}
}

func TestConfigureDockerAuth_DryRunNoExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := ConfigureDockerAuth(context.Background(), "dryrun-host.pkg.dev"); err != nil {
		t.Fatalf("ConfigureDockerAuth dry-run = %v, want nil (echo, no exec)", err)
	}
}

func TestGetGKECredentials_DryRunNoExec(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "")
	t.Setenv("SPARKWING_DRY_RUN", "1")
	err := GetGKECredentials(context.Background(), GKEConfig{Cluster: "dry", Location: "us-west1", Project: "p"})
	if err != nil {
		t.Fatalf("GetGKECredentials dry-run = %v, want nil (echo, no exec)", err)
	}
}
