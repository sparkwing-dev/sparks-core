package jobs

import (
	"strings"
	"testing"
)

func TestRefusePostV0(t *testing.T) {
	cases := []struct {
		version string
		wantErr string // substring; empty = no error
	}{
		{version: "v0.1.0", wantErr: ""},
		{version: "v0.6.1", wantErr: ""},
		{version: "v0.99.999", wantErr: ""},
		{version: "v1.0.0", wantErr: "pre-1.0 lock"},
		{version: "v1.2.3", wantErr: "pre-1.0 lock"},
		{version: "v2.0.0", wantErr: "pre-1.0 lock"},
	}
	for _, c := range cases {
		t.Run(c.version, func(t *testing.T) {
			err := refusePostV0(c.version)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.wantErr)
			}
		})
	}
}
