// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// TestIntakeStoreFailureIncrementsFailedDecision is the #9811 seeded red and
// green pair for the EshuTriggerHandoffFailed alert: a forced store failure on
// every intake route must increment
// eshu_dp_webhook_trigger_decisions_total{status="failed"}, the series the
// freshness alert and dashboard query, plus the request, store-operation and
// duration series an operator uses to localise it.
func TestIntakeStoreFailureIncrementsFailedDecision(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			stores := testStores()
			route.failStore(stores)
			mux := harness.server(stores.stores(), 0).Mux()

			rec := postIntake(t, mux, route.path, route.payload)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("store failure = %d (%s), want 500", rec.Code, rec.Body.String())
			}

			if got := harness.counter(t, metricDecisions,
				decisionAttrs(route.provider, route.eventKind, "accepted", "none", "failed")); got != 1 {
				t.Fatalf("failed trigger decisions = %d, want 1", got)
			}
			if got := harness.counter(t, metricRequests,
				requestAttrs(route.provider, "failed", "store_failed")); got != 1 {
				t.Fatalf("failed requests = %d, want 1", got)
			}
			if got := harness.counter(t, metricStoreOps,
				storeAttrs(route.provider, "failed", "failed")); got != 1 {
				t.Fatalf("failed store operations = %d, want 1", got)
			}
			if got := harness.histogramCount(t, metricStoreDur,
				storeAttrs(route.provider, "failed", "failed")); got != 1 {
				t.Fatalf("store duration observations = %d, want 1", got)
			}
			if got := harness.histogramCount(t, metricReqDur,
				requestAttrs(route.provider, "failed", "store_failed")); got != 1 {
				t.Fatalf("request duration observations = %d, want 1", got)
			}
		})
	}
}

// TestIntakeStoreSuccessIncrementsDecisionAndRequest is the green side of the
// pair: a stored trigger on every route counts one stored request and one
// non-failed decision, so the alert stays quiet on healthy traffic.
func TestIntakeStoreSuccessIncrementsDecisionAndRequest(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			mux := harness.server(testStores().stores(), 0).Mux()

			rec := postIntake(t, mux, route.path, route.payload)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d (%s), want 202", rec.Code, rec.Body.String())
			}

			if got := harness.counter(t, metricDecisions,
				decisionAttrs(route.provider, route.eventKind, "accepted", "none", "queued")); got != 1 {
				t.Fatalf("queued trigger decisions = %d, want 1", got)
			}
			if got := harness.counter(t, metricDecisions,
				decisionAttrs(route.provider, route.eventKind, "accepted", "none", "failed")); got != 0 {
				t.Fatalf("failed trigger decisions = %d, want 0 on a healthy store", got)
			}
			if got := harness.counter(t, metricRequests,
				requestAttrs(route.provider, "stored", "none")); got != 1 {
				t.Fatalf("stored requests = %d, want 1", got)
			}
			if got := harness.counter(t, metricStoreOps,
				storeAttrs(route.provider, "stored", "queued")); got != 1 {
				t.Fatalf("stored store operations = %d, want 1", got)
			}
			if got := harness.histogramCount(t, metricStoreDur,
				storeAttrs(route.provider, "stored", "queued")); got != 1 {
				t.Fatalf("store duration observations = %d, want 1", got)
			}
			if got := harness.histogramCount(t, metricReqDur,
				requestAttrs(route.provider, "stored", "none")); got != 1 {
				t.Fatalf("request duration observations = %d, want 1", got)
			}
		})
	}
}

// statusRepositoryStore returns a fixed stored status so the decision series
// can be checked for every durable lifecycle state the store reports.
type statusRepositoryStore struct{ status webhook.TriggerStatus }

func (s statusRepositoryStore) StoreTrigger(_ context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	return webhook.StoredTrigger{Trigger: trigger, TriggerID: "t-1", Status: s.status}, nil
}

// TestIntakeDecisionStatusMatchesStoredStatus pins that the status label is
// the stored lifecycle state, not the request outcome.
func TestIntakeDecisionStatusMatchesStoredStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []webhook.TriggerStatus{
		webhook.TriggerStatusQueued, webhook.TriggerStatusIgnored, webhook.TriggerStatusHandedOff,
	} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			stores := testStores().stores()
			stores.Repository = statusRepositoryStore{status: status}
			mux := harness.server(stores, 0).Mux()

			rec := postIntake(t, mux, intakeRoutes()[0].path, intakeRoutes()[0].payload)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("intake = %d, want 202", rec.Code)
			}
			if got := harness.counter(t, metricDecisions,
				decisionAttrs("github", "push", "accepted", "none", string(status))); got != 1 {
				t.Fatalf("decisions with status %q = %d, want 1", status, got)
			}
		})
	}
}

// TestIntakeUnknownStoredStatusIsBounded pins that a status outside the
// durable lifecycle collapses to "unknown" instead of minting a series.
func TestIntakeUnknownStoredStatusIsBounded(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	stores := testStores().stores()
	stores.Repository = statusRepositoryStore{status: "weird-status"}
	mux := harness.server(stores, 0).Mux()

	rec := postIntake(t, mux, intakeRoutes()[0].path, intakeRoutes()[0].payload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d, want 202", rec.Code)
	}
	if got := harness.counter(t, metricDecisions,
		decisionAttrs("github", "push", "accepted", "none", "unknown")); got != 1 {
		t.Fatalf("decisions with status=unknown = %d, want 1", got)
	}
}

// TestIntakeIgnoredDecisionCarriesNormalizerReason pins decision and reason
// passthrough for the known normalizer reasons.
func TestIntakeIgnoredDecisionCarriesNormalizerReason(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	mux := harness.server(testStores().stores(), 0).Mux()
	payload := cloneRoute(intakeRoutes()[0].payload)
	payload["decision"] = "ignored"
	payload["reason"] = "non_default_branch"

	rec := postIntake(t, mux, intakeRoutes()[0].path, payload)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake = %d, want 202", rec.Code)
	}
	if got := harness.counter(t, metricDecisions,
		decisionAttrs("github", "push", "ignored", "non_default_branch", "ignored")); got != 1 {
		t.Fatalf("ignored decisions = %d, want 1", got)
	}
}

// TestIntakeRejectionsCountRequestsOnly pins one request series per rejection
// reason with bounded labels, and no decision or store series: a rejected
// request never reached storage.
func TestIntakeRejectionsCountRequestsOnly(t *testing.T) {
	t.Parallel()

	repo := intakeRoutes()[0]
	versioned := cloneRoute(repo.payload)
	versioned["api_version"] = "v2"
	mismatch := cloneRoute(repo.payload)
	mismatch["provider"] = "pagerduty"
	invalid := cloneRoute(repo.payload)
	invalid["event_kind"] = "bogus"

	cases := []struct {
		name         string
		method, auth string
		body         []byte
		maxBody      int64
		provider     string
		reason       string
	}{
		{"missing_bearer", http.MethodPost, "", marshalRoute(t, repo.payload), 0, "unknown", "auth_failed"},
		{"wrong_bearer", http.MethodPost, "Bearer nope", marshalRoute(t, repo.payload), 0, "unknown", "auth_failed"},
		{"bad_method", http.MethodGet, "Bearer core-intake-token", marshalRoute(t, repo.payload), 0, "unknown", "bad_method"},
		{"body_too_large", http.MethodPost, "Bearer core-intake-token", []byte(strings.Repeat("x", 65)), 64, "unknown", "body_too_large"},
		{"empty_body", http.MethodPost, "Bearer core-intake-token", nil, 0, "unknown", "empty_body"},
		{"invalid_json", http.MethodPost, "Bearer core-intake-token", []byte("{not json"), 0, "unknown", "malformed_event"},
		{"unsupported_api_version", http.MethodPost, "Bearer core-intake-token", marshalRoute(t, versioned), 0, "unknown", "unsupported_api_version"},
		{"unsupported_provider", http.MethodPost, "Bearer core-intake-token", marshalRoute(t, mismatch), 0, "unknown", "unsupported_provider"},
		{"invalid_trigger", http.MethodPost, "Bearer core-intake-token", marshalRoute(t, invalid), 0, "unknown", "invalid_trigger"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			stores := testStores()
			mux := harness.server(stores.stores(), tc.maxBody).Mux()

			rec := rawIntake(mux, tc.method, repo.path, tc.auth, tc.body)
			if rec.Code < 400 || rec.Code >= 500 {
				t.Fatalf("rejection = %d, want 4xx", rec.Code)
			}
			if got := harness.counter(t, metricRequests, requestAttrs(tc.provider, "rejected", tc.reason)); got != 1 {
				t.Fatalf("rejected requests{reason=%s} = %d, want 1", tc.reason, got)
			}
			if got := harness.histogramCount(t, metricReqDur, requestAttrs(tc.provider, "rejected", tc.reason)); got != 1 {
				t.Fatalf("request duration observations = %d, want 1", got)
			}
			for _, name := range []string{metricDecisions, metricStoreOps} {
				if got := harness.seriesCount(t, name); got != 0 {
					t.Fatalf("%s series = %d, want 0 for a rejected request", name, got)
				}
			}
			if stores.repository.calls != 0 {
				t.Fatalf("store calls = %d, want 0", stores.repository.calls)
			}
		})
	}
}

// TestIntakeRoutesCountEveryRouteRequest pins that all five intake routes
// count a rejected request under their own provider label.
func TestIntakeRoutesCountEveryRouteRequest(t *testing.T) {
	t.Parallel()

	for _, route := range intakeRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			harness := newMetricsHarness(t)
			mux := harness.server(testStores().stores(), 0).Mux()

			rec := rawIntake(mux, http.MethodPost, route.path, "", marshalRoute(t, route.payload))
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated = %d, want 401", rec.Code)
			}
			provider := route.provider
			if route.name == "repository" || route.name == "incident" {
				provider = "unknown" // provider is only trusted after validation
			}
			if got := harness.counter(t, metricRequests, requestAttrs(provider, "rejected", "auth_failed")); got != 1 {
				t.Fatalf("auth_failed requests = %d, want 1", got)
			}
		})
	}
}

// TestIntakeActionsConflictCountsRejectedRequest pins the 409 path: the store
// ran, so its duration is observed, but a delivery conflict is a rejection and
// not a failed handoff, so no failed decision is emitted.
func TestIntakeActionsConflictCountsRejectedRequest(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	stores := testStores()
	stores.actions.err = webhook.ErrActionsDeliveryConflict
	mux := harness.server(stores.stores(), 0).Mux()
	route := intakeRoutes()[4]

	rec := postIntake(t, mux, route.path, route.payload)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict = %d, want 409", rec.Code)
	}
	if got := harness.counter(t, metricRequests, requestAttrs("github", "rejected", "delivery_conflict")); got != 1 {
		t.Fatalf("delivery_conflict requests = %d, want 1", got)
	}
	if got := harness.seriesCount(t, metricDecisions); got != 0 {
		t.Fatalf("decision series = %d, want 0 for a conflict", got)
	}
	if got := harness.histogramCount(t, metricStoreDur, storeAttrs("github", "rejected", "unknown")); got != 1 {
		t.Fatalf("store duration observations = %d, want 1", got)
	}
}

// TestIntakeUnconfiguredStoreIsAFailedHandoff pins that a 503 for a route with
// no store is a failed handoff the alert can see.
func TestIntakeUnconfiguredStoreIsAFailedHandoff(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	stores := testStores().stores()
	stores.AWS = nil
	mux := harness.server(stores, 0).Mux()
	route := intakeRoutes()[2]

	rec := postIntake(t, mux, route.path, route.payload)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured store = %d, want 503", rec.Code)
	}
	if got := harness.counter(t, metricDecisions,
		decisionAttrs("aws_freshness", "config_change", "accepted", "none", "failed")); got != 1 {
		t.Fatalf("failed decisions = %d, want 1", got)
	}
	if got := harness.counter(t, metricRequests, requestAttrs("aws_freshness", "failed", "store_failed")); got != 1 {
		t.Fatalf("failed requests = %d, want 1", got)
	}
}

// TestIntakeMetricLabelsStayBounded pins that wire strings the satellite
// controls (decision reason, incident event kind) cannot mint new series, and
// that no delivery id or repository name ever becomes a label value.
func TestIntakeMetricLabelsStayBounded(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	mux := harness.server(testStores().stores(), 0).Mux()
	repo := cloneRoute(intakeRoutes()[0].payload)
	repo["decision"] = "ignored"
	incident := cloneRoute(intakeRoutes()[1].payload)
	for i := 0; i < 5; i++ {
		repo["reason"] = "hostile-reason-" + string(rune('a'+i))
		repo["delivery_id"] = "delivery-secret-" + string(rune('a'+i))
		repo["repository_full_name"] = "octo/private-" + string(rune('a'+i))
		incident["event_kind"] = "incident.custom-" + string(rune('a'+i))
		incident["event_id"] = "evt-" + string(rune('a'+i))
		postIntake(t, mux, intakeRoutes()[0].path, repo)
		postIntake(t, mux, intakeRoutes()[1].path, incident)
	}

	if got := harness.counter(t, metricDecisions,
		decisionAttrs("github", "push", "ignored", "other", "ignored")); got != 5 {
		t.Fatalf("repository decisions with reason=other = %d, want 5", got)
	}
	if got := harness.counter(t, metricDecisions,
		decisionAttrs("pagerduty", "other", "accepted", "none", "queued")); got != 5 {
		t.Fatalf("incident decisions with event_kind=other = %d, want 5", got)
	}
	if got := harness.seriesCount(t, metricDecisions); got != 2 {
		t.Fatalf("decision series = %d, want 2 (hostile strings must collapse)", got)
	}
	for _, sm := range harness.collect(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			if strings.Contains(strings.ToLower(describe(m.Data)), "secret") ||
				strings.Contains(describe(m.Data), "octo/") {
				t.Fatalf("metric %s leaked a wire identity into labels: %s", m.Name, describe(m.Data))
			}
		}
	}
}

// TestIntakeHealthzEmitsNoWebhookSeries pins that the unauthenticated probe is
// not an intake route and stays out of the request series.
func TestIntakeHealthzEmitsNoWebhookSeries(t *testing.T) {
	t.Parallel()

	harness := newMetricsHarness(t)
	mux := harness.server(testStores().stores(), 0).Mux()
	rec := rawIntake(mux, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz = %d, want 200", rec.Code)
	}
	if got := harness.seriesCount(t, metricRequests); got != 0 {
		t.Fatalf("request series after healthz = %d, want 0", got)
	}
}

func cloneRoute(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func marshalRoute(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}
