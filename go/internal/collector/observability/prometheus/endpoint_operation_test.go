// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheus

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestCollectTargetsFailureCarriesEndpointToken proves issue #8258: a failed
// targets fetch must record its bounded endpoint token so failure_operation
// carries the endpoint (list_targets), not the claim-stage fallback. The
// request count also pins the retry contract: HTTP 500 stays retryable under
// maxHTTPRetries.
func TestCollectTargetsFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"error","errorType":"internal"}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result := CollectionResult{}
	err = client.collectTargets(t.Context(), TargetConfig{InstanceID: "prom-main"}, &result)
	if err == nil {
		t.Fatal("collectTargets() error = nil, want provider failure")
	}
	if got, want := requests, maxHTTPRetries+1; got != want {
		t.Fatalf("requests = %d, want %d (retry contract must not change)", got, want)
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "list_targets"; got != want {
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
