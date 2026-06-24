// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseBaseURLRejectsCredentialBearingURL(t *testing.T) {
	t.Parallel()

	_, err := ParseBaseURL("grafana", "https://user:pass@grafana.example.test")
	if err == nil {
		t.Fatal("ParseBaseURL() error = nil, want credentials rejected")
	}
}

func TestParseRetryAfterHandlesSecondsAndHTTPDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		raw  string
		want time.Duration
	}{
		{name: "seconds", raw: "7", want: 7 * time.Second},
		{name: "http date", raw: now.Add(3 * time.Second).Format(http.TimeFormat), want: 3 * time.Second},
		{name: "past date", raw: now.Add(-time.Second).Format(http.TimeFormat), want: 0},
		{name: "invalid", raw: "not-a-date", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ParseRetryAfter(tt.raw, now); got != tt.want {
				t.Fatalf("ParseRetryAfter(%q) = %s, want %s", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHTTPErrorExposesRetryAfterDelay(t *testing.T) {
	t.Parallel()

	err := HTTPError{Provider: "test", StatusCode: http.StatusTooManyRequests, RetryAfter: 17 * time.Second}
	if got, want := err.RetryAfterDelay(), 17*time.Second; got != want {
		t.Fatalf("RetryAfterDelay() = %s, want %s", got, want)
	}
}

func TestStatusPolicyClassifiesSharedProviderStatuses(t *testing.T) {
	t.Parallel()

	policy := StatusPolicy{
		AuthDeniedClass: FailurePermissionHidden,
		NotFoundClass:   FailureDeleted,
		GoneClass:       FailureArchived,
	}
	tests := []struct {
		name     string
		status   int
		want     FailureClass
		terminal bool
	}{
		{name: "permission hidden", status: http.StatusForbidden, want: FailurePermissionHidden, terminal: true},
		{name: "deleted", status: http.StatusNotFound, want: FailureDeleted, terminal: true},
		{name: "archived", status: http.StatusGone, want: FailureArchived, terminal: true},
		{name: "rate limited", status: http.StatusTooManyRequests, want: FailureRateLimited},
		{name: "server error", status: http.StatusBadGateway, want: FailureRetryable},
		{name: "bad request", status: http.StatusBadRequest, want: FailureTerminal, terminal: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := policy.ClassifyStatus(tt.status)
			if got.Class != tt.want {
				t.Fatalf("Class = %q, want %q", got.Class, tt.want)
			}
			if got.Terminal != tt.terminal {
				t.Fatalf("Terminal = %v, want %v", got.Terminal, tt.terminal)
			}
		})
	}
}

func TestClassifyProviderFailurePreservesRetryAfterDelay(t *testing.T) {
	t.Parallel()

	cause := HTTPError{Provider: "test", StatusCode: http.StatusTooManyRequests, RetryAfter: 23 * time.Second}
	failure := ClassifyProviderFailure("test", cause, StatusPolicy{}, FailureRetryable)
	if got, want := failure.RetryAfterDelay(), 23*time.Second; got != want {
		t.Fatalf("RetryAfterDelay() = %s, want %s", got, want)
	}
	if got, want := failure.FailureClass(), string(FailureRateLimited); got != want {
		t.Fatalf("FailureClass() = %q, want %q", got, want)
	}
}

func TestDoJSONRetriesRetryableStatusAndClosesBodies(t *testing.T) {
	t.Parallel()

	var attempts int
	var retryStatuses []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("retry body must be closed"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	err = DoJSON(context.Background(), JSONRequest{
		Provider:   "test",
		Method:     http.MethodGet,
		BaseURL:    base,
		Endpoint:   "/metadata",
		Client:     server.Client(),
		Out:        &out,
		MaxRetries: 2,
		OnRetry: func(resp *http.Response, _ int) {
			retryStatuses = append(retryStatuses, resp.StatusCode)
		},
	})
	if err != nil {
		t.Fatalf("DoJSON() error = %v, want nil", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if !out.OK {
		t.Fatal("decoded OK = false, want true")
	}
	if got, want := len(retryStatuses), 2; got != want {
		t.Fatalf("retry statuses = %d, want %d", got, want)
	}
}

func TestDoJSONReturnsBoundedHTTPErrorWithoutResponseBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "11")
		w.Header().Set("RateLimit-Reason", "burst")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("token secret should not surface"))
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	err = DoJSON(context.Background(), JSONRequest{
		Provider:   "test",
		Method:     http.MethodGet,
		BaseURL:    base,
		Endpoint:   "/metadata",
		Client:     server.Client(),
		MaxRetries: 0,
	})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want HTTPError")
	}
	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("DoJSON() error = %T, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusTooManyRequests)
	}
	if httpErr.RetryAfter != 11*time.Second {
		t.Fatalf("RetryAfter = %s, want 11s", httpErr.RetryAfter)
	}
	if httpErr.RateLimitReason != "burst" {
		t.Fatalf("RateLimitReason = %q, want burst", httpErr.RateLimitReason)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked response body: %q", err.Error())
	}
}

func TestDoJSONUsesCustomDecoder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte("status: success\n"))
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	var decoded string
	err = DoJSON(context.Background(), JSONRequest{
		Provider: "test",
		Method:   http.MethodGet,
		BaseURL:  base,
		Endpoint: "/metadata",
		Client:   server.Client(),
		Decode: func(body io.Reader) error {
			data, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			decoded = string(data)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("DoJSON() error = %v, want nil", err)
	}
	if decoded != "status: success\n" {
		t.Fatalf("decoded body = %q, want custom decoder body", decoded)
	}
}

func TestDoJSONReturnsHandledStatusSentinel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("hidden provider detail"))
	}))
	defer server.Close()

	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	err = DoJSON(context.Background(), JSONRequest{
		Provider: "test",
		Method:   http.MethodGet,
		BaseURL:  base,
		Endpoint: "/metadata",
		Client:   server.Client(),
		StatusError: func(*http.Response) error {
			return ErrStatusHandled
		},
	})
	if !errors.Is(err, ErrStatusHandled) {
		t.Fatalf("DoJSON() error = %v, want ErrStatusHandled", err)
	}
	if strings.Contains(err.Error(), "hidden provider detail") {
		t.Fatalf("handled status leaked response body: %q", err.Error())
	}
}
