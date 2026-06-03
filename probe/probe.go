// Package probe builds HTTP health probes for post-deploy verification.
// A probe's Check method is a func(ctx) error suitable for sparkwing's
// Job.Verify: it returns nil when the target is healthy, and otherwise
// an error that Indeterminate classifies as either a definitive
// "unhealthy" response or a "could not determine" failure (transport,
// auth, timeout). Recovery logic needs that distinction: roll back on a
// definitive unhealthy result, escalate when the check merely could not
// run.
//
//	sw.Job(plan, "deploy", &Deploy{}).
//	    Verify(probe.HTTP("https://svc/healthz").
//	        HeaderFunc("X-Service-Token", signToken).
//	        ExpectJSON("status", "ok").
//	        Retry(30).Interval(2 * time.Second).Check)
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type header struct {
	name  string
	value string
	fn    func(ctx context.Context) (string, error)
}

type jsonExpect struct {
	path  string
	value any
}

// HTTPProbe is an HTTP health check built via [HTTP] and the chainable
// option methods, terminated by [HTTPProbe.Check].
type HTTPProbe struct {
	url        string
	method     string
	body       []byte
	headers    []header
	expectCode int // 0 = any 2xx
	jsonChecks []jsonExpect
	retries    int
	interval   time.Duration
	timeout    time.Duration
}

// HTTP starts an HTTP probe against url. Defaults: GET, 2s interval
// between retries, 10s per-request timeout, no retries, any 2xx accepted.
func HTTP(url string) *HTTPProbe {
	return &HTTPProbe{
		url:      url,
		method:   http.MethodGet,
		interval: 2 * time.Second,
		timeout:  10 * time.Second,
	}
}

// Method sets the HTTP method (default GET).
func (p *HTTPProbe) Method(m string) *HTTPProbe { p.method = m; return p }

// Body sets a request body (e.g. for a POST health check).
func (p *HTTPProbe) Body(b []byte) *HTTPProbe { p.body = b; return p }

// Header adds a static request header.
func (p *HTTPProbe) Header(name, value string) *HTTPProbe {
	p.headers = append(p.headers, header{name: name, value: value})
	return p
}

// HeaderFunc adds a header whose value is computed on every request
// attempt. Use it for credentials that expire: a signed token captured
// once would go stale during a multi-minute retry loop and make the
// probe fail on auth rather than health.
func (p *HTTPProbe) HeaderFunc(name string, fn func(ctx context.Context) (string, error)) *HTTPProbe {
	p.headers = append(p.headers, header{name: name, fn: fn})
	return p
}

// ExpectStatus requires an exact status code. Unset accepts any 2xx.
func (p *HTTPProbe) ExpectStatus(code int) *HTTPProbe { p.expectCode = code; return p }

// ExpectJSON requires the JSON response body to have value at the given
// dotted path (e.g. "status" or "data.ready"). Multiple calls accumulate
// (all must hold). Comparison is by string form, so ExpectJSON("n", 200)
// matches a JSON number 200.
func (p *HTTPProbe) ExpectJSON(path string, value any) *HTTPProbe {
	p.jsonChecks = append(p.jsonChecks, jsonExpect{path: path, value: value})
	return p
}

// Retry sets how many additional attempts to make after the first.
func (p *HTTPProbe) Retry(n int) *HTTPProbe {
	if n < 0 {
		n = 0
	}
	p.retries = n
	return p
}

// Interval sets the delay between attempts.
func (p *HTTPProbe) Interval(d time.Duration) *HTTPProbe { p.interval = d; return p }

// Timeout sets the per-request timeout.
func (p *HTTPProbe) Timeout(d time.Duration) *HTTPProbe { p.timeout = d; return p }

// probeError carries the indeterminate-vs-definitive classification read
// back by Indeterminate.
type probeError struct {
	indeterminate bool
	msg           string
	cause         error
}

func (e *probeError) Error() string {
	if e.cause != nil {
		return e.msg + ": " + e.cause.Error()
	}
	return e.msg
}

func (e *probeError) Unwrap() error { return e.cause }

// Indeterminate reports whether err means the probe could not determine
// health (transport error, auth failure, timeout, unreadable/undecodable
// body) rather than a definitive unhealthy response. Recovery logic
// should escalate on indeterminate and only treat a non-indeterminate
// failure as grounds to roll back. Returns false for nil and for
// non-probe errors.
func Indeterminate(err error) bool {
	var pe *probeError
	if errors.As(err, &pe) {
		return pe.indeterminate
	}
	return false
}

// Check runs the probe, retrying up to Retry times with Interval between
// attempts, and returns nil once the target is healthy. On exhaustion it
// returns the last failure; classify it with [Indeterminate]. Suitable
// as a sparkwing Job.Verify check.
func (p *HTTPProbe) Check(ctx context.Context) error {
	client := &http.Client{Timeout: p.timeout}
	var last error
	attempts := p.retries + 1
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-time.After(p.interval):
			case <-ctx.Done():
				return &probeError{indeterminate: true, msg: "probe cancelled", cause: ctx.Err()}
			}
		}
		if err := p.once(ctx, client); err != nil {
			last = err
			continue
		}
		return nil
	}
	return last
}

func (p *HTTPProbe) once(ctx context.Context, client *http.Client) error {
	var bodyReader io.Reader
	if len(p.body) > 0 {
		bodyReader = bytes.NewReader(p.body)
	}
	req, err := http.NewRequestWithContext(ctx, p.method, p.url, bodyReader)
	if err != nil {
		return &probeError{indeterminate: true, msg: "build request", cause: err}
	}
	for _, h := range p.headers {
		val := h.value
		if h.fn != nil {
			v, ferr := h.fn(ctx)
			if ferr != nil {
				return &probeError{indeterminate: true, msg: "header " + h.name + " provider", cause: ferr}
			}
			val = v
		}
		req.Header.Set(h.name, val)
	}
	resp, err := client.Do(req)
	if err != nil {
		return &probeError{indeterminate: true, msg: "request failed", cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return &probeError{indeterminate: true, msg: "read response body", cause: err}
	}
	// Auth failures are about our credentials, not the target's health.
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &probeError{indeterminate: true, msg: fmt.Sprintf("auth failed: HTTP %d", resp.StatusCode)}
	}
	if p.expectCode == 0 {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &probeError{msg: fmt.Sprintf("unhealthy: HTTP %d", resp.StatusCode)}
		}
	} else if resp.StatusCode != p.expectCode {
		return &probeError{msg: fmt.Sprintf("unhealthy: HTTP %d (want %d)", resp.StatusCode, p.expectCode)}
	}
	if len(p.jsonChecks) > 0 {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return &probeError{indeterminate: true, msg: "decode JSON body", cause: err}
		}
		for _, jc := range p.jsonChecks {
			got, ok := jsonPath(decoded, jc.path)
			if !ok {
				return &probeError{msg: fmt.Sprintf("unhealthy: JSON path %q missing", jc.path)}
			}
			if fmt.Sprint(got) != fmt.Sprint(jc.value) {
				return &probeError{msg: fmt.Sprintf("unhealthy: JSON %q = %v, want %v", jc.path, got, jc.value)}
			}
		}
	}
	return nil
}

// jsonPath walks a dotted path through decoded JSON objects.
func jsonPath(v any, path string) (any, bool) {
	cur := v
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
