package gcp

import (
	"reflect"
	"testing"
)

func clearProjectEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
}

func TestResolveProject_ExplicitWins(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")
	if got := ResolveProject("from-arg"); got != "from-arg" {
		t.Fatalf("ResolveProject = %q, want from-arg (explicit wins)", got)
	}
}

func TestResolveProject_GoogleCloudProjectEnv(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "env-proj")
	if got := ResolveProject(""); got != "env-proj" {
		t.Fatalf("ResolveProject = %q, want env-proj", got)
	}
}

func TestResolveProject_CloudSDKEnvFallback(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_CORE_PROJECT", "sdk-proj")
	if got := ResolveProject(""); got != "sdk-proj" {
		t.Fatalf("ResolveProject = %q, want sdk-proj", got)
	}
}

func TestResolveProject_GoogleCloudProjectBeatsCloudSDK(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "primary")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "secondary")
	if got := ResolveProject(""); got != "primary" {
		t.Fatalf("ResolveProject = %q, want primary", got)
	}
}

func TestResolveProject_EmptyWhenUnset(t *testing.T) {
	clearProjectEnv(t)
	if got := ResolveProject(""); got != "" {
		t.Fatalf("ResolveProject = %q, want empty", got)
	}
}

func TestProjectArgs_ResolvedProject(t *testing.T) {
	clearProjectEnv(t)
	got := ProjectArgs("my-proj")
	want := []string{"--project", "my-proj"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectArgs = %v, want %v", got, want)
	}
}

func TestProjectArgs_NilWhenUnresolved(t *testing.T) {
	clearProjectEnv(t)
	if got := ProjectArgs(""); got != nil {
		t.Fatalf("ProjectArgs = %v, want nil (ADC fallback)", got)
	}
}

func clearIdentityEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
}

func TestIsWorkloadIdentity_InClusterNoKeyFile(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if !IsWorkloadIdentity() {
		t.Fatal("IsWorkloadIdentity = false, want true (in-cluster, no key file)")
	}
}

func TestIsWorkloadIdentity_KeyFileWins(t *testing.T) {
	clearIdentityEnv(t)
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/var/run/key.json")
	if IsWorkloadIdentity() {
		t.Fatal("IsWorkloadIdentity = true, want false (explicit key file)")
	}
}

func TestIsWorkloadIdentity_LocalIsFalse(t *testing.T) {
	clearIdentityEnv(t)
	if IsWorkloadIdentity() {
		t.Fatal("IsWorkloadIdentity = true, want false (local, no cluster)")
	}
}

func TestImpersonationArgs_SetFromEnv(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "deployer@proj.iam.gserviceaccount.com")
	got := ImpersonationArgs()
	want := []string{"--impersonate-service-account", "deployer@proj.iam.gserviceaccount.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImpersonationArgs = %v, want %v", got, want)
	}
}

func TestImpersonationArgs_NilWhenUnset(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "")
	if got := ImpersonationArgs(); got != nil {
		t.Fatalf("ImpersonationArgs = %v, want nil", got)
	}
}
