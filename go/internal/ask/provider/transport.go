// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpDoer is the minimal HTTP client interface used by transport.
// Accepting an interface rather than *http.Client allows tests to inject a
// httptest.Server client directly.
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// transport is a thin HTTP client wrapper that adds a bounded retry policy and
// leak-free error reporting. It retries on 429 and 5xx responses up to
// maxRetries additional attempts (so a single call can make at most
// maxRetries+1 total requests). Response bodies are never included in error
// messages: only the numeric status code is reported.
type transport struct {
	client     httpDoer
	maxRetries int
}

// newTransport returns a transport using client as the HTTP doer. When client
// is nil a default *http.Client with a 60-second timeout is used. maxRetries
// is set to 2, allowing up to 3 total attempts.
func newTransport(client httpDoer) *transport {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &transport{
		client:     client,
		maxRetries: 2,
	}
}

// postJSON marshals reqBody as JSON and POSTs it to url. It sets the
// Content-Type header to application/json and applies any additional headers
// from headers. On a 2xx response the body is decoded into out. On a 429 or
// 5xx response the call is retried up to t.maxRetries times. Any other
// non-2xx response is returned as a *ProviderError; the raw response body is
// drained and discarded so it never appears in the returned error.
func (t *transport) postJSON(ctx context.Context, url string, headers map[string]string, reqBody any, out any) error {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("ask/provider: marshal request: %w", err)
	}

	attempt := 0
	for {
		resp, doErr := t.doOnce(ctx, url, headers, raw)
		if doErr != nil {
			return doErr
		}

		statusCode := resp.StatusCode

		// On retryable status codes, drain the body and retry if we have
		// remaining attempts. A tiny fixed pause keeps retries respectful
		// without introducing real-time variance in tests.
		if shouldRetry(statusCode) && attempt < t.maxRetries {
			drainAndClose(resp.Body)
			attempt++
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay(attempt)):
			}
			continue
		}

		if statusCode >= 200 && statusCode < 300 {
			decErr := json.NewDecoder(resp.Body).Decode(out)
			drainAndClose(resp.Body)
			if decErr != nil {
				return fmt.Errorf("ask/provider: decode response: %w", decErr)
			}
			return nil
		}

		// Non-2xx, non-retryable: drain and discard the body so it never
		// leaks into the error string.
		drainAndClose(resp.Body)
		return &ProviderError{
			StatusCode: statusCode,
			Message:    fmt.Sprintf("provider returned HTTP %d", statusCode),
		}
	}
}

// postJSONStream marshals reqBody as JSON and POSTs it to url, returning the
// response body open for incremental reading (for streaming responses). The
// caller is responsible for draining and closing the returned body. Only 2xx
// responses are returned; any non-2xx response causes an error, and the body
// is drained and discarded so it never leaks into the error value. No automatic
// retry is applied for streaming calls because the response body is consumed
// incrementally and cannot be safely replayed.
func (t *transport) postJSONStream(ctx context.Context, url string, headers map[string]string, reqBody any) (io.ReadCloser, error) {
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ask/provider: marshal stream request: %w", err)
	}
	resp, err := t.doOnce(ctx, url, headers, raw)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		drainAndClose(resp.Body)
		return nil, &ProviderError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("provider returned HTTP %d", resp.StatusCode),
		}
	}
	return resp.Body, nil
}

// doOnce builds and executes a single POST request.
func (t *transport) doOnce(ctx context.Context, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ask/provider: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask/provider: execute request: %w", err)
	}
	return resp, nil
}

// shouldRetry reports whether statusCode warrants an automatic retry.
// Only 429 (rate limit) and 5xx (server error) are retried; 4xx errors other
// than 429 are caller errors and must not be silently swallowed.
func shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || (statusCode >= 500 && statusCode < 600)
}

// retryDelay returns the pause before retry number n (1-based). It uses a
// small fixed value so the retry loop stays deterministic and fast in tests.
// No real-time jitter is applied; providers that require exponential back-off
// should inject it at a higher layer.
func retryDelay(n int) time.Duration {
	// 50 ms per retry is negligible in the human-latency sense and keeps the
	// test suite well under any timeout.
	return time.Duration(n) * 50 * time.Millisecond
}

// drainAndClose reads up to 4 KiB from r then closes it, preventing
// connection leaks without including the body content in any error path.
func drainAndClose(r io.ReadCloser) {
	if r == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 4*1024))
	_ = r.Close()
}

// ProviderError is returned by postJSON when the upstream provider responds
// with a non-2xx status code. The raw response body is never included in
// Message or Error() to prevent leaking provider internals.
type ProviderError struct {
	// StatusCode is the HTTP status code returned by the provider.
	StatusCode int
	// Message is a short, bounded description of the failure containing only
	// the status code. It never includes the raw response body.
	Message string
	// cause is the underlying error, if any, available via Unwrap.
	cause error
}

// Error implements the error interface. The returned string contains only the
// status code; it never includes raw provider response bodies.
func (e *ProviderError) Error() string {
	return e.Message
}

// Unwrap returns the underlying cause, enabling errors.Is and errors.As
// traversal through the error chain.
func (e *ProviderError) Unwrap() error {
	return e.cause
}
