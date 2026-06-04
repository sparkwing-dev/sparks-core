package services

import "testing"

func TestParseHostPort(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"ipv4 binding", "127.0.0.1:49161", 49161, false},
		{"trailing newline", "127.0.0.1:5599\n", 5599, false},
		{"two bindings takes first", "0.0.0.0:32768\n[::]:32768", 32768, false},
		{"empty", "", 0, true},
		{"no colon", "garbage", 0, true},
		{"non-numeric port", "127.0.0.1:abc", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHostPort(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseHostPort(%q) err = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("parseHostPort(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestPostgresDSN(t *testing.T) {
	got := PostgresDSN("app", "secret", 54320, "appdb")
	want := "postgres://app:secret@localhost:54320/appdb?sslmode=disable"
	if got != want {
		t.Errorf("PostgresDSN = %q, want %q", got, want)
	}
}

func TestSpec_Defaults(t *testing.T) {
	s := Spec{}
	s.defaults()
	if s.Name == "" {
		t.Error("expected a generated Name")
	}
	if s.ReadyTimeout == 0 {
		t.Error("expected a default ReadyTimeout")
	}
}
