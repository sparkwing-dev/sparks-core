package probe_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sparkwing-dev/sparks-core/probe"
)

func TestCheck_HealthyPasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	err := probe.HTTP(srv.URL).ExpectJSON("status", "ok").Check(context.Background())
	if err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
}

func TestCheck_UnhealthyBodyIsDefinitive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer srv.Close()

	err := probe.HTTP(srv.URL).ExpectJSON("status", "ok").Check(context.Background())
	if err == nil {
		t.Fatal("expected unhealthy error")
	}
	if probe.Indeterminate(err) {
		t.Fatalf("a valid response that fails the assertion is definitive, not indeterminate: %v", err)
	}
}

func TestCheck_AuthFailureIsIndeterminate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := probe.HTTP(srv.URL).Retry(1).Interval(time.Millisecond).Check(context.Background())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !probe.Indeterminate(err) {
		t.Fatalf("auth failure should be indeterminate (don't roll back), got definitive: %v", err)
	}
}

func TestCheck_RetriesUntilHealthy(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	err := probe.HTTP(srv.URL).
		ExpectJSON("status", "ok").
		Retry(5).Interval(time.Millisecond).
		Check(context.Background())
	if err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 attempts (2 unhealthy + 1 healthy), got %d", got)
	}
}

func TestCheck_PerAttemptHeader(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Token") == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := probe.HTTP(srv.URL).
		HeaderFunc("X-Token", func(_ context.Context) (string, error) {
			return fmt.Sprintf("tok-%d", calls.Add(1)), nil
		}).
		Check(context.Background())
	if err != nil {
		t.Fatalf("expected healthy with per-attempt token, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected header provider called once, got %d", calls.Load())
	}
}
