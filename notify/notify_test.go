package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhook_PostsJSON(t *testing.T) {
	var gotBody map[string]string
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Slack(context.Background(), srv.URL, "hello"); err != nil {
		t.Fatalf("Slack: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["text"] != "hello" {
		t.Errorf("body text = %q, want hello", gotBody["text"])
	}
}

func TestWebhook_NonOKIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if err := Slack(context.Background(), srv.URL, "x"); err == nil {
		t.Fatal("expected error on non-2xx response")
	}
}

func TestWebhook_EmptyURLSkips(t *testing.T) {
	if err := Webhook(context.Background(), WebhookConfig{URL: ""}); err != nil {
		t.Errorf("empty URL should be a no-op, got %v", err)
	}
}
