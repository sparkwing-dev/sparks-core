package coverage

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return data
}

func absFixture(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("abs fixture %s: %v", rel, err)
	}
	return abs
}

func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestParseGoProfile(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    float64
		wantErr bool
	}{
		{"statement weighted total", "go/valid.out", 80.0, false},
		{"count mode all covered", "go/full.out", 100.0, false},
		{"only mode line", "go/empty.out", 0, true},
		{"non numeric statement count", "go/malformed.out", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseGoProfile(readFixture(t, tc.fixture))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !approx(got, tc.want) {
				t.Fatalf("total = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseLCOV(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    float64
		wantErr bool
	}{
		{"hit over found across records", "lcov/valid.info", 60.0, false},
		{"no LF records", "lcov/nolf.info", 0, true},
		{"non numeric count", "lcov/malformed.info", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLCOV(readFixture(t, tc.fixture))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !approx(got, tc.want) {
				t.Fatalf("total = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseLCOV_RejectsHitAboveFound(t *testing.T) {
	_, err := parseLCOV([]byte("SF:x\nLF:2\nLH:5\nend_of_record\n"))
	if err == nil {
		t.Fatal("expected error when hit lines exceed found lines")
	}
}

func TestParseCobertura(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    float64
		wantErr bool
	}{
		{"root line-rate", "cobertura/valid.xml", 75.0, false},
		{"lines covered over valid fallback", "cobertura/linecount.xml", 75.0, false},
		{"truncated xml", "cobertura/malformed.xml", 0, true},
		{"wrong root element", "cobertura/wrongroot.xml", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCobertura(readFixture(t, tc.fixture))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !approx(got, tc.want) {
				t.Fatalf("total = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseCobertura_RejectsOutOfRangeLineRate(t *testing.T) {
	_, err := parseCobertura([]byte(`<coverage line-rate="1.5"/>`))
	if err == nil {
		t.Fatal("expected error for line-rate above 1")
	}
}

func TestTotal_DispatchesByFormat(t *testing.T) {
	cases := []struct {
		name    string
		report  Report
		want    float64
		wantErr bool
	}{
		{"empty format defaults to go", Report{Path: absFixture(t, "go/valid.out")}, 80.0, false},
		{"explicit go", Report{Format: "go", Path: absFixture(t, "go/valid.out")}, 80.0, false},
		{"lcov", Report{Format: "LCOV", Path: absFixture(t, "lcov/valid.info")}, 60.0, false},
		{"cobertura", Report{Format: "cobertura", Path: absFixture(t, "cobertura/valid.xml")}, 75.0, false},
		{"unknown format", Report{Format: "clover", Path: absFixture(t, "go/valid.out")}, 0, true},
		{"missing file", Report{Format: "go", Path: absFixture(t, "go/does-not-exist.out")}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Total(context.Background(), tc.report)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && !approx(got, tc.want) {
				t.Fatalf("total = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGateAtLeast_PassesAtOrAboveFloor(t *testing.T) {
	r := Report{Format: "go", Path: absFixture(t, "go/valid.out")}
	if err := GateAtLeast(80, r)(context.Background()); err != nil {
		t.Fatalf("expected pass at floor, got %v", err)
	}
	if err := GateAtLeast(50, r)(context.Background()); err != nil {
		t.Fatalf("expected pass below floor, got %v", err)
	}
}

func TestGateAtLeast_FailsBelowFloor(t *testing.T) {
	r := Report{Format: "go", Path: absFixture(t, "go/valid.out")}
	err := GateAtLeast(90, r)(context.Background())
	if err == nil {
		t.Fatal("expected failure below floor")
	}
	if !strings.Contains(err.Error(), "80.0%") || !strings.Contains(err.Error(), "90.0%") {
		t.Fatalf("error should name measured total and floor, got %q", err)
	}
}

func TestGateAtLeast_PropagatesParseError(t *testing.T) {
	r := Report{Format: "go", Path: absFixture(t, "go/malformed.out")}
	if err := GateAtLeast(1, r)(context.Background()); err == nil {
		t.Fatal("expected parse error to propagate through the gate")
	}
}
