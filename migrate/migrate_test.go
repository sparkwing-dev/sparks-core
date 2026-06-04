package migrate

import (
	"strings"
	"testing"
)

func TestArgs_SourceAndDatabaseFlags(t *testing.T) {
	got := args(Config{Dir: "db/migrations", DSN: "postgres://localhost/x"}, "up")
	want := []string{
		"-source", "file://db/migrations",
		"-database", "postgres://localhost/x",
		"up",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestConfig_DefaultsBinary(t *testing.T) {
	c := Config{}
	c.defaults()
	if c.Binary != "migrate" {
		t.Errorf("default Binary = %q, want migrate", c.Binary)
	}
}

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"both set", Config{Dir: "d", DSN: "x"}, false},
		{"no dir", Config{DSN: "x"}, true},
		{"no dsn", Config{Dir: "d"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("validate() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
