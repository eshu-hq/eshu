// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestSearchIssuesFailureCarriesEndpointToken proves issue #8258: a failed
// provider endpoint call must record its bounded endpoint token so
// failure_operation carries the endpoint (search_issues), not the claim-stage
// fallback.
func TestSearchIssuesFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorMessages":["boom"]}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "test-token"})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	now := time.Date(2026, time.May, 31, 19, 0, 0, 0, time.UTC)
	window := CollectionWindow{Since: now.Add(-time.Hour), Until: now}
	stats := CollectionStats{}
	_, _, err = client.searchIssues(context.Background(), TargetConfig{IssueLimit: 25}, window, &stats)
	if err == nil {
		t.Fatal("searchIssues() error = nil, want provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "search_issues"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
	if got, want := failure.FailureClass(), string(sdk.FailureRetryable); got != want {
		t.Fatalf("FailureClass() = %q, want %q (retry contract must not change)", got, want)
	}
	if failure.TerminalFailure() {
		t.Fatal("TerminalFailure() = true, want false for HTTP 500 (retry contract must not change)")
	}
	if got, want := failure.FailureStatusCode(), http.StatusInternalServerError; got != want {
		t.Fatalf("FailureStatusCode() = %d, want %d", got, want)
	}
}

// TestSearchIssuesArchivedFailureCarriesEndpointToken proves the #8399 P2: a
// 410 archived response from the search endpoint must record search_issues,
// not the claim-stage fallback, while keeping the archived class and terminal
// contract.
func TestSearchIssuesArchivedFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"errorMessages":["archived"]}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "test-token"})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	now := time.Date(2026, time.May, 31, 19, 0, 0, 0, time.UTC)
	window := CollectionWindow{Since: now.Add(-time.Hour), Until: now}
	stats := CollectionStats{}
	_, _, err = client.searchIssues(context.Background(), TargetConfig{IssueLimit: 25}, window, &stats)
	if err == nil {
		t.Fatal("searchIssues() error = nil, want provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "search_issues"; got != want {
		t.Fatalf("FailureOperation() = %q, want %q", got, want)
	}
	if got, want := failure.FailureClass(), string(sdk.FailureArchived); got != want {
		t.Fatalf("FailureClass() = %q, want %q (archived contract must not change)", got, want)
	}
	if !failure.TerminalFailure() {
		t.Fatal("TerminalFailure() = false, want true for HTTP 410 (archived contract must not change)")
	}
}
