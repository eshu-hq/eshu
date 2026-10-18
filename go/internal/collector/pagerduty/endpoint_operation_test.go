// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pagerduty

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// TestListIncidentsFailureCarriesEndpointToken proves issue #8258: a failed
// incidents listing must record its bounded endpoint token so
// failure_operation carries the endpoint (list_incidents), not the
// claim-stage fallback.
// TestPagerDutyOperationForPathUnknownServiceSubPathFallsBackToBlank proves
// the #8399 P2: an unknown `/services/{id}/<other>` sub-path must keep a
// blank token (claim-stage fallback) instead of mislabeling as read_service.
// Only the exact read shape and the known /integrations listing pin tokens.
func TestPagerDutyOperationForPathUnknownServiceSubPathFallsBackToBlank(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/services":                        "list_services",
		"/services/P123":                   "read_service",
		"/services/P123/integrations":      "list_service_integrations",
		"/services/P123/future_subpath":    "",
		"/services/P123/nested/deeper":     "",
		"/incidents/INC1/some_future_view": "",
		"/incidents/INC1/log_entries":      "list_log_entries",
		"/totally_unknown":                 "",
	}
	for path, want := range cases {
		if got := pagerDutyOperationForPath(path); got != want {
			t.Errorf("pagerDutyOperationForPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestListIncidentsFailureCarriesEndpointToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(HTTPClientConfig{BaseURL: server.URL, Token: "pd-token", Client: server.Client()})
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v, want nil", err)
	}
	now := testObservedAt()
	window := CollectionWindow{Since: now.Add(-time.Hour), Until: now}
	target := TargetConfig{IncidentLimit: 50}
	_, _, _, err = client.listIncidents(t.Context(), target, window, paginationBoundsForTarget(target))
	if err == nil {
		t.Fatal("listIncidents() error = nil, want provider failure")
	}
	failure := classifiedProviderFailure(err)
	if got, want := failure.FailureOperation(), "list_incidents"; got != want {
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
