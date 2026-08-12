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

// The caller's project is the only one passed. gcloud still reads
// CLOUDSDK_CORE_PROJECT itself when no flag is given, with its own
// precedence.
func TestProjectArgs_NoCallerProjectLeavesEnvToGcloud(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("CLOUDSDK_CORE_PROJECT", "sdk-proj")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "primary")
	if got := ProjectArgs(""); got != nil {
		t.Fatalf("ProjectArgs(\"\") with project env set = %v, want nil", got)
	}
}

func TestProjectArgs_CallerBeatsEnv(t *testing.T) {
	clearProjectEnv(t)
	t.Setenv("GOOGLE_CLOUD_PROJECT", "from-env")
	got := ProjectArgs("from-caller")
	want := []string{"--project", "from-caller"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectArgs = %v, want %v", got, want)
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

func TestImpersonationArgs_Named(t *testing.T) {
	got := ImpersonationArgs("deployer@proj.iam.gserviceaccount.com")
	want := []string{"--impersonate-service-account", "deployer@proj.iam.gserviceaccount.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImpersonationArgs = %v, want %v", got, want)
	}
}

// An inherited variable must not decide which identity a deploy runs
// as, so an unnamed account passes no flag and leaves gcloud its own
// handling of CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT.
func TestImpersonationArgs_NilWhenUnnamed(t *testing.T) {
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "deployer@proj.iam.gserviceaccount.com")
	if got := ImpersonationArgs(""); got != nil {
		t.Fatalf("ImpersonationArgs(\"\") = %v, want nil", got)
	}
}
