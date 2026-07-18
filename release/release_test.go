package release

import (
	"context"
	"testing"
)

func TestIsSemver(t *testing.T) {
	cases := []struct {
		in    string
		valid bool
	}{
		{"1.2.3", true},
		{"v1.2.3", true},
		{"0.0.0", true},
		{"1.2.3-rc.1", true},
		{"v1.2.3-alpha.1+build.7", true},
		{"1.2.3+meta", true},
		{"1.2", false},
		{"1", false},
		{"v1", false},
		{"1.2.3.4", false},
		{"01.2.3", false},
		{"1.2.3-", false},
		{"", false},
		{"latest", false},
		{"vv1.2.3", false},
	}
	for _, tc := range cases {
		if got := IsSemver(tc.in); got != tc.valid {
			t.Errorf("IsSemver(%q) = %v, want %v", tc.in, got, tc.valid)
		}
	}
}

func TestDeriveVersion_ExplicitParamWins(t *testing.T) {
	got, err := DeriveVersion(context.Background(), VersionConfig{Version: "v2.3.4", Describe: true})
	if err != nil {
		t.Fatalf("DeriveVersion: %v", err)
	}
	if got != "v2.3.4" {
		t.Errorf("version = %q, want v2.3.4", got)
	}
}

func TestDeriveVersion_RejectsNonSemver(t *testing.T) {
	_, err := DeriveVersion(context.Background(), VersionConfig{Version: "not-a-version"})
	if err == nil {
		t.Fatal("expected error for non-semver version")
	}
}

func TestDeriveVersion_AllowNonSemver(t *testing.T) {
	got, err := DeriveVersion(context.Background(), VersionConfig{Version: "2024.07.01", AllowNonSemver: true})
	if err != nil {
		t.Fatalf("DeriveVersion: %v", err)
	}
	if got != "2024.07.01" {
		t.Errorf("version = %q, want 2024.07.01", got)
	}
}

func TestDeriveVersion_DevFallbackWhenNoVersion(t *testing.T) {
	got, err := DeriveVersion(context.Background(), VersionConfig{DevFallback: "0.0.0-dev"})
	if err != nil {
		t.Fatalf("DeriveVersion: %v", err)
	}
	if got != "0.0.0-dev" {
		t.Errorf("version = %q, want 0.0.0-dev", got)
	}
}

func TestDeriveVersion_NoVersionNoFallbackErrors(t *testing.T) {
	_, err := DeriveVersion(context.Background(), VersionConfig{})
	if err == nil {
		t.Fatal("expected error when nothing resolves a version")
	}
}
