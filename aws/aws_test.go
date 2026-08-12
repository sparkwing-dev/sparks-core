package aws

import (
	"reflect"
	"testing"
)

func awsEnvOff(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_PROFILE", "")
}

func TestProfileArgs_ConfiguredProfile(t *testing.T) {
	awsEnvOff(t)
	got := ProfileArgs("ci")
	want := []string{"--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileArgs(\"ci\") = %v, want %v", got, want)
	}
}

func TestProfileArgs_EmptyProfileOmitsFlag(t *testing.T) {
	awsEnvOff(t)
	if got := ProfileArgs(""); got != nil {
		t.Fatalf("ProfileArgs(\"\") = %v, want nil (ambient credential chain)", got)
	}
}

func TestProfileArgs_CallerWinsOverAWSProfileEnv(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_PROFILE", "from-env")
	got := ProfileArgs("ci")
	want := []string{"--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProfileArgs(\"ci\") with AWS_PROFILE set = %v, want %v", got, want)
	}
}

// An unnamed profile passes no flag even when AWS_PROFILE is set, so the
// aws CLI applies its own precedence: environment keys outrank the named
// profile, which is what boto3 does and what an assumed role in CI needs.
func TestProfileArgs_NoCallerProfileLeavesAWSProfileToTheCLI(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_PROFILE", "from-env")
	if got := ProfileArgs(""); got != nil {
		t.Fatalf("ProfileArgs(\"\") with AWS_PROFILE set = %v, want nil", got)
	}
}

func TestProfileArgs_IRSADropsProfile(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	if got := ProfileArgs("ci"); got != nil {
		t.Fatalf("ProfileArgs under IRSA = %v, want nil", got)
	}
}

func TestProfileFlag_ConfiguredProfile(t *testing.T) {
	awsEnvOff(t)
	if got, want := ProfileFlag("ci"), " --profile ci"; got != want {
		t.Fatalf("ProfileFlag(\"ci\") = %q, want %q", got, want)
	}
}

func TestProfileFlag_EmptyProfileOmitsFlag(t *testing.T) {
	awsEnvOff(t)
	if got := ProfileFlag(""); got != "" {
		t.Fatalf("ProfileFlag(\"\") = %q, want \"\" (ambient credential chain)", got)
	}
}

func TestProfileFlag_IRSAReturnsEmpty(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	if got := ProfileFlag("ci"); got != "" {
		t.Fatalf("ProfileFlag under IRSA = %q, want \"\"", got)
	}
}

func TestIsIRSA(t *testing.T) {
	awsEnvOff(t)
	if IsIRSA() {
		t.Fatal("IsIRSA = true with no token file env")
	}
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	if !IsIRSA() {
		t.Fatal("IsIRSA = false with token file env set")
	}
}

func TestCallerIdentityArgs_WithProfile(t *testing.T) {
	awsEnvOff(t)
	got := CallerIdentityArgs("ci")
	want := []string{"sts", "get-caller-identity", "--query", "Account", "--output", "text", "--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CallerIdentityArgs(\"ci\") = %v, want %v", got, want)
	}
}

func TestCallerIdentityArgs_WithoutProfile(t *testing.T) {
	awsEnvOff(t)
	got := CallerIdentityArgs("")
	want := []string{"sts", "get-caller-identity", "--query", "Account", "--output", "text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CallerIdentityArgs(\"\") = %v, want %v", got, want)
	}
}
