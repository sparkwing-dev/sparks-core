// Package notify posts deploy and run notifications to an HTTP webhook. An
// empty URL warns rather than erroring, so a missing webhook never fails the
// recovery path it is wired into.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/step"
)

type WebhookConfig struct {
	// URL empty skips the notification with a warning.
	URL string
	// Payload is JSON-marshaled into the request body.
	Payload any
	// Headers add to the request; Content-Type defaults to application/json.
	Headers map[string]string
	// Timeout defaults to 10s.
	Timeout time.Duration
}

// Webhook POSTs cfg.Payload as JSON to cfg.URL, erroring on a non-2xx response.
func Webhook(ctx context.Context, cfg WebhookConfig) error {
	if cfg.URL == "" {
		sparkwing.Warn(ctx, "notify: no webhook URL configured - skipping notification")
		return nil
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 10 * time.Second
	}
	return step.Run(ctx, "notify", func(ctx context.Context) error {
		body, err := json.Marshal(cfg.Payload)
		if err != nil {
			return fmt.Errorf("notify: marshal payload: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("notify: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range cfg.Headers {
			req.Header.Set(k, v)
		}
		client := &http.Client{Timeout: cfg.Timeout}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("notify: POST %s: %w", cfg.URL, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			return fmt.Errorf("notify: webhook returned HTTP %d: %s", resp.StatusCode, string(snippet))
		}
		return nil
	})
}

// Slack posts text as the {"text": ...} shape most chat webhooks accept.
func Slack(ctx context.Context, webhookURL, text string) error {
	return Webhook(ctx, WebhookConfig{
		URL:     webhookURL,
		Payload: map[string]string{"text": text},
	})
}
