// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempo

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestCollectTagsFailureCarriesEndpointToken proves issue #8258: a failed tag
// search must record its bounded endpoint token so failure_operation carries
// the endpoint (list_tags), not the claim-stage fallback.
func TestCollectTagsFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	result := CollectionResult{ObservedAt: time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)}
	_, err = client.collectTags(t.Context(), TargetConfig{InstanceID: "tempo-main"}, &result)
	if err == nil {
		t.Fatal("collectTags() error = nil, want provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "list_tags"; got != want {
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

// TestTempoOperationForEndpointRequiresValuesSuffix proves the #8399 P2: a
// future sub-path under the tag-values prefix must not silently inherit the
// list_tag_values token. Only the exact /values shape carries it.
func TestTempoOperationForEndpointRequiresValuesSuffix(t *testing.T) {
	t.Parallel()

	if got := tempoOperationForEndpoint(tagValuesPrefix + "http.method" + "/values"); got != "list_tag_values" {
		t.Fatalf("tempoOperationForEndpoint(values path) = %q, want %q", got, "list_tag_values")
	}
	if got := tempoOperationForEndpoint(tagValuesPrefix + "http.method" + "/other"); got != "" {
		t.Fatalf("tempoOperationForEndpoint(non-values sub-path) = %q, want blank", got)
	}
}
