// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

var errPostgresDown = errors.New("postgres unavailable")

const (
	metricRequests  = "eshu_dp_webhook_requests_total"
	metricDecisions = "eshu_dp_webhook_trigger_decisions_total"
	metricStoreOps  = "eshu_dp_webhook_store_operations_total"
	metricReqDur    = "eshu_dp_webhook_request_duration_seconds"
	metricStoreDur  = "eshu_dp_webhook_store_duration_seconds"
)

// metricsHarness wires a real SDK meter so tests read back exactly what an
// operator's Prometheus scrape would see.
type metricsHarness struct {
	reader *sdkmetric.ManualReader
	inst   *telemetry.Instruments
}

func newMetricsHarness(t *testing.T) *metricsHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("intake-metrics-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return &metricsHarness{reader: reader, inst: inst}
}

func (h *metricsHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}

func attrsEqual(set attribute.Set, want map[string]string) bool {
	if set.Len() != len(want) {
		return false
	}
	for key, value := range want {
		got, ok := set.Value(attribute.Key(key))
		if !ok || got.AsString() != value {
			return false
		}
	}
	return true
}

// counter returns the summed counter value whose attribute set equals attrs
// exactly: an extra or missing label is a different series, as in Prometheus.
func (h *metricsHarness) counter(t *testing.T, name string, attrs map[string]string) int64 {
	t.Helper()
	var total int64
	for _, sm := range h.collect(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != name {
				continue
			}
			for _, point := range sum.DataPoints {
				if attrsEqual(point.Attributes, attrs) {
					total += point.Value
				}
			}
		}
	}
	return total
}

// histogramCount returns the observation count of the series whose
// attribute set equals attrs exactly.
func (h *metricsHarness) histogramCount(t *testing.T, name string, attrs map[string]string) uint64 {
	t.Helper()
	var total uint64
	for _, sm := range h.collect(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok || m.Name != name {
				continue
			}
			for _, point := range hist.DataPoints {
				if attrsEqual(point.Attributes, attrs) {
					total += point.Count
				}
			}
		}
	}
	return total
}

// seriesCount returns how many distinct label sets a metric has recorded.
func (h *metricsHarness) seriesCount(t *testing.T, name string) int {
	t.Helper()
	count := 0
	for _, sm := range h.collect(t).ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				count += len(data.DataPoints)
			case metricdata.Histogram[float64]:
				count += len(data.DataPoints)
			}
		}
	}
	return count
}

func (h *metricsHarness) server(stores Stores, maxBody int64) *Server {
	return NewServer(ServerConfig{
		Token:        "core-intake-token",
		MaxBodyBytes: maxBody,
		Instruments:  h.inst,
	}, stores)
}

func requestAttrs(provider, outcome, reason string) map[string]string {
	return map[string]string{"provider": provider, "outcome": outcome, "reason": reason}
}

func decisionAttrs(provider, eventKind, decision, reason, status string) map[string]string {
	return map[string]string{
		"provider": provider, "event_kind": eventKind, "decision": decision,
		"reason": reason, "status": status,
	}
}

func storeAttrs(provider, outcome, status string) map[string]string {
	return map[string]string{"provider": provider, "outcome": outcome, "status": status}
}

// intakeRoute describes one intake route with a valid payload, the labels a
// successful decision must carry, and a hook that forces its store to fail.
type intakeRoute struct {
	name      string
	path      string
	payload   map[string]any
	provider  string
	eventKind string
	failStore func(*testStoreSet)
}

func intakeRoutes() []intakeRoute {
	return []intakeRoute{
		{
			name: "repository", path: "/api/v1/webhook/intake/repository",
			payload: map[string]any{
				"api_version": "v1", "provider": "github", "delivery_id": "delivery-1",
				"event_kind": "push", "decision": "accepted", "repository_external_id": "123",
				"repository_full_name": "octo/repo", "default_branch": "main", "target_sha": "abc123",
			},
			provider: "github", eventKind: "push",
			failStore: func(s *testStoreSet) { s.repository.err = errPostgresDown },
		},
		{
			name: "incident", path: "/api/v1/webhook/intake/incident",
			payload: map[string]any{
				"api_version": "v1", "provider": "pagerduty", "event_kind": "incident.triggered",
				"event_id": "evt-1", "scope_id": "scope-1", "resource_id": "res-1",
				"observed_at": "2026-05-15T10:00:00Z",
			},
			provider: "pagerduty", eventKind: "incident.triggered",
			failStore: func(s *testStoreSet) { s.incident.err = errPostgresDown },
		},
		{
			name: "aws", path: "/api/v1/webhook/intake/aws",
			payload: map[string]any{
				"api_version": "v1", "event_id": "evt-1", "kind": "config_change",
				"account_id": "123456789012", "region": "us-east-1", "service_kind": "lambda",
				"resource_type": "aws_lambda_function", "resource_id": "fn-1",
				"observed_at": "2026-05-15T10:00:00Z",
			},
			provider: "aws_freshness", eventKind: "config_change",
			failStore: func(s *testStoreSet) { s.aws.err = errPostgresDown },
		},
		{
			name: "gcp", path: "/api/v1/webhook/intake/gcp",
			payload: map[string]any{
				"api_version": "v1", "event_id": "evt-1", "kind": string(catalog.EventKindAssetChange),
				"parent_scope_kind": string(catalog.ParentScopeProject), "parent_scope_id": "demo-project",
				"asset_type": "compute.googleapis.com/Instance", "location": "us-central1-a",
				"observed_at": "2026-05-15T10:00:00Z",
			},
			provider: "gcp_freshness", eventKind: string(catalog.EventKindAssetChange),
			failStore: func(s *testStoreSet) { s.gcp.err = errPostgresDown },
		},
		{
			name: "actions", path: "/api/v1/webhook/intake/actions",
			payload: map[string]any{
				"api_version": "v1", "provider": "github", "delivery_id": "delivery-1",
				"event_kind": "workflow_run", "action": "completed", "repository_id": "123",
				"repository": "octo/repo", "run_id": "42",
			},
			provider: "github", eventKind: "workflow_run",
			failStore: func(s *testStoreSet) { s.actions.err = errPostgresDown },
		},
	}
}

func rawIntake(mux http.Handler, method, path, auth string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// describe renders a metric's data points for label-leak assertions.
func describe(data metricdata.Aggregation) string {
	switch typed := data.(type) {
	case metricdata.Sum[int64]:
		var b strings.Builder
		for _, point := range typed.DataPoints {
			b.WriteString(point.Attributes.Encoded(attribute.DefaultEncoder()))
			b.WriteString(";")
		}
		return b.String()
	case metricdata.Histogram[float64]:
		var b strings.Builder
		for _, point := range typed.DataPoints {
			b.WriteString(point.Attributes.Encoded(attribute.DefaultEncoder()))
			b.WriteString(";")
		}
		return b.String()
	}
	return ""
}
