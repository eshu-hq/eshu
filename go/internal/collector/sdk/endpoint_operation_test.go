// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestDoJSONCarriesOperationIntoClassifiedFailure proves issue #8258: the
// bounded endpoint token on a JSONRequest must reach the classified failure
// so failure_operation records the endpoint, not the claim-stage fallback.
func TestDoJSONCarriesOperationIntoClassifiedFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error"}`))
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	err = DoJSON(context.Background(), JSONRequest{
		Provider:  "jira",
		BaseURL:   base,
		Endpoint:  "/rest/api/3/search/jql",
		Operation: "search_issues",
		Client:    server.Client(),
	})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want provider failure")
	}
	var httpErr HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("DoJSON() error type = %T, want HTTPError", err)
	}
	if got, want := httpErr.Operation, "search_issues"; got != want {
		t.Fatalf("HTTPError.Operation = %q, want %q", got, want)
	}
	failure := ClassifyProviderFailure("jira", err, StatusPolicy{}, FailureRetryable)
	if got, want := failure.FailureOperation(), "search_issues"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
	if got, want := failure.FailureClass(), string(FailureRetryable); got != want {
		t.Fatalf("FailureClass() = %q, want %q (retry contract must not change)", got, want)
	}
	if failure.TerminalFailure() {
		t.Fatal("TerminalFailure() = true, want false for HTTP 500 (retry contract must not change)")
	}
	if got, want := failure.FailureStatusCode(), http.StatusInternalServerError; got != want {
		t.Fatalf("FailureStatusCode() = %d, want %d", got, want)
	}
}

// TestDoJSONWithoutOperationKeepsStageFallback proves a call with no endpoint
// token still classifies with a blank operation so claim handling falls back
// to its stage token.
func TestDoJSONWithoutOperationKeepsStageFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	err = DoJSON(context.Background(), JSONRequest{
		Provider: "jira",
		BaseURL:  base,
		Endpoint: "/rest/api/3/search/jql",
		Client:   server.Client(),
	})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want provider failure")
	}
	failure := ClassifyProviderFailure("jira", err, StatusPolicy{}, FailureRetryable)
	if got := failure.FailureOperation(); got != "" {
		t.Fatalf("FailureOperation() = %q, want blank for the stage fallback", got)
	}
	if got, want := failure.FailureClass(), string(FailureTerminal); got != want {
		t.Fatalf("FailureClass() = %q, want %q (retry contract must not change)", got, want)
	}
}

// TestDoJSONPreservesHookSetOperation proves a custom StatusError hook that
// sets its own token wins over the request token.
func TestDoJSONPreservesHookSetOperation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	err = DoJSON(context.Background(), JSONRequest{
		Provider:  "jira",
		BaseURL:   base,
		Endpoint:  "/rest/api/3/search/jql",
		Operation: "search_issues",
		Client:    server.Client(),
		StatusError: func(resp *http.Response) error {
			return HTTPError{Provider: "jira", StatusCode: resp.StatusCode, Message: "custom", Operation: "hook_token"}
		},
	})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want provider failure")
	}
	failure := ClassifyProviderFailure("jira", err, StatusPolicy{}, FailureRetryable)
	if got, want := failure.FailureOperation(), "hook_token"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q (hook token wins)", got, want)
	}
}

// TestDoJSONStatusHandledCarriesNoFailure proves the partial-coverage
// sentinel still short-circuits without becoming a failure.
func TestDoJSONStatusHandledCarriesNoFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	err = DoJSON(context.Background(), JSONRequest{
		Provider:  "grafana",
		BaseURL:   base,
		Endpoint:  "/api/search",
		Operation: "search_resources",
		Client:    server.Client(),
		StatusError: func(*http.Response) error {
			return ErrStatusHandled
		},
	})
	if !errors.Is(err, ErrStatusHandled) {
		t.Fatalf("DoJSON() error = %v, want ErrStatusHandled", err)
	}
}

// TestDoJSONTransportFailureCarriesOperation proves transport failures (the
// retry path) also record the endpoint token.
func TestDoJSONTransportFailureCarriesOperation(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("parse test URL: %v", err)
	}
	err = DoJSON(context.Background(), JSONRequest{
		Provider:  "tempo",
		BaseURL:   base,
		Endpoint:  "/api/v2/search/tags",
		Operation: "list_tags",
	})
	if err == nil {
		t.Fatal("DoJSON() error = nil, want transport failure")
	}
	failure := ClassifyProviderFailure("tempo", err, StatusPolicy{}, FailureRetryable)
	if got, want := failure.FailureOperation(), "list_tags"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
	if got, want := failure.FailureClass(), string(FailureRetryable); got != want {
		t.Fatalf("FailureClass() = %q, want %q (retry contract must not change)", got, want)
	}
}

// TestClassifyPropagatesWrappedOperation proves a non-HTTP carrier (such as
// a provider API-status error) still reaches the classified failure.
func TestClassifyPropagatesWrappedOperation(t *testing.T) {
	t.Parallel()

	cause := operationCarrier{operation: "list_targets", err: errors.New("bad_data")}
	failure := ClassifyProviderFailure("metric", cause, StatusPolicy{}, FailureRetryable)
	if got, want := failure.FailureOperation(), "list_targets"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
}

// operationCarrier is a test-only FailureOperation carrier standing in for
// provider API-status errors.
type operationCarrier struct {
	operation string
	err       error
}

func (c operationCarrier) Error() string { return "carrier: " + c.err.Error() }

func (c operationCarrier) Unwrap() error { return c.err }

func (c operationCarrier) FailureOperation() string { return c.operation }
