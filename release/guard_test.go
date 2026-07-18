package release

import (
	"testing"
)

func TestDefaultVersionField(t *testing.T) {
	cases := map[string]string{
		"package.json":   "version",
		"pyproject.toml": "project.version",
		"deno.json":      "version",
	}
	for path, want := range cases {
		if got := defaultVersionField(path); got != want {
			t.Errorf("defaultVersionField(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestExtractJSONField(t *testing.T) {
	data := []byte(`{"name":"pkg","version":"1.4.2","nested":{"v":"9.9.9"}}`)
	cases := []struct {
		field   string
		want    string
		wantErr bool
	}{
		{"version", "1.4.2", false},
		{"nested.v", "9.9.9", false},
		{"missing", "", true},
		{"nested.missing", "", true},
		{"name.deep", "", true},
	}
	for _, tc := range cases {
		got, err := extractJSONField(data, tc.field)
		if (err != nil) != tc.wantErr {
			t.Errorf("extractJSONField(%q) err = %v, wantErr %v", tc.field, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("extractJSONField(%q) = %q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestExtractTOMLField(t *testing.T) {
	data := []byte(`# a pyproject
[build-system]
requires = ["hatchling"]

[project]
name = "pkg"
version = "3.1.0"  # inline comment ignored

[tool.poetry]
version = '2.0.1'
`)
	cases := []struct {
		field   string
		want    string
		wantErr bool
	}{
		{"project.version", "3.1.0", false},
		{"project.name", "pkg", false},
		{"tool.poetry.version", "2.0.1", false},
		{"project.missing", "", true},
		{"absent.version", "", true},
	}
	for _, tc := range cases {
		got, err := extractTOMLField(data, tc.field)
		if (err != nil) != tc.wantErr {
			t.Errorf("extractTOMLField(%q) err = %v, wantErr %v", tc.field, err, tc.wantErr)
			continue
		}
		if got != tc.want {
			t.Errorf("extractTOMLField(%q) = %q, want %q", tc.field, got, tc.want)
		}
	}
}

func TestExtractTOMLField_HashInQuotedValueKept(t *testing.T) {
	data := []byte("[project]\ndescription = \"has # hash\"\nversion = \"1.0.0\"\n")
	got, err := extractTOMLField(data, "project.description")
	if err != nil {
		t.Fatalf("extractTOMLField: %v", err)
	}
	if got != "has # hash" {
		t.Errorf("value = %q, want %q", got, "has # hash")
	}
}

func TestExtractVersionField_UnsupportedExt(t *testing.T) {
	_, err := extractVersionField("setup.cfg", []byte("x"), "version")
	if err == nil {
		t.Fatal("expected error for unsupported file extension")
	}
}
