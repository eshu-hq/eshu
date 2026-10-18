// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafana

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestCollectSearchResourcesFailureCarriesEndpointToken proves issue #8258: a
// failed dashboard search must record its bounded endpoint token so
// failure_operation carries the endpoint (search_resources), not the
// claim-stage fallback.
func TestCollectSearchResourcesFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "grafana-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result := CollectionResult{}
	err = client.collectSearchResources(t.Context(), TargetConfig{}, &result)
	if err == nil {
		t.Fatal("collectSearchResources() error = nil, want provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "search_resources"; got != want {
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
