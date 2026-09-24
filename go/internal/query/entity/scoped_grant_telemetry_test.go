// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const queryScopedGrantDeniedMetric = "eshu_dp_query_scoped_grant_denied_total"

// newTestInstruments builds a real OTEL Instruments backed by a manual
// reader, mirroring deployment's sibling test helper of the same name
// (different package, so no import cycle) so both #6786 R2-4 emission seams
// are proven against the actual wire-level counter.
func newTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("entity-scoped-grant-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func scopedGrantDeniedDataPoints(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.DataPoint[int64] {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var points []metricdata.DataPoint[int64]
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != queryScopedGrantDeniedMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", m.Name, m.Data)
			}
			points = append(points, sum.DataPoints...)
		}
	}
	return points
}

func attrString(t *testing.T, dp metricdata.DataPoint[int64], dimensionKey string) string {
	t.Helper()
	got, ok := dp.Attributes.Value(attribute.Key(dimensionKey))
	if !ok {
		t.Fatalf("%s data point has no %q attribute", queryScopedGrantDeniedMetric, dimensionKey)
	}
	return got.AsString()
}

// TestFetchWorkloadContextForOperationAnchorMismatchEmitsTelemetry is the
// #6786 R2-4 proof for workload_context.go's F3 guard: a row whose own
// id/name disagrees with the requested selector must count
// reason=backend_anchor_mismatch.
func TestFetchWorkloadContextForOperationAnchorMismatchEmitsTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	graph := graph.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		return map[string]any{"id": "workload:different", "name": "different", "repo_id": "repo-a"}, nil
	}}
	handler := &Handler{Neo4j: graph, Logger: logger, Instruments: instruments}

	got, err := handler.FetchWorkloadContextForOperation(
		context.Background(), "w.id = $service_name", map[string]any{"service_name": "workload:requested"}, "workload_context",
	)
	if err != nil {
		t.Fatalf("FetchWorkloadContextForOperation() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("FetchWorkloadContextForOperation() = %#v, want nil", got)
	}
	if !strings.Contains(logBuf.String(), "backend_anchor_mismatch") {
		t.Fatalf("log output = %q, want a backend_anchor_mismatch Warn", logBuf.String())
	}

	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "backend_anchor_mismatch"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), "workload_context"; got != want {
		t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestFetchWorkloadContextForOperationGrantDeniedEmitsTelemetry is the
// ordinary-denial half: a row that matches its own anchor but whose
// repository the scoped caller has no grant to (directly or via DEFINES)
// must count reason=grant_denied.
func TestFetchWorkloadContextForOperationGrantDeniedEmitsTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			return map[string]any{"id": "workload:out-of-grant", "name": "workload:out-of-grant", "repo_id": "repo-b"}, nil
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			return nil, nil // no DEFINES candidates: FetchWorkloadRepositoryForAccess resolves "".
		},
	}
	handler := &Handler{Neo4j: graph, Instruments: instruments}
	ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	})

	got, err := handler.FetchWorkloadContextForOperation(
		ctx, "w.id = $service_name", map[string]any{"service_name": "workload:out-of-grant"}, "workload_context",
	)
	if err != nil {
		t.Fatalf("FetchWorkloadContextForOperation() error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("FetchWorkloadContextForOperation() = %#v, want nil", got)
	}

	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "grant_denied"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), "workload_context"; got != want {
		t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestGetEntityContextAnchorMismatchEmitsTelemetry is the entity/handler.go
// F5 guard's #6786 R2-4 proof: a graph row whose own id disagrees with the
// requested entity id must count reason=backend_anchor_mismatch, over HTTP,
// the real production call path.
func TestGetEntityContextAnchorMismatchEmitsTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	graph := graph.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		return map[string]any{
			"id": "entity-different", "labels": []any{"Function"}, "name": "different",
			"repo_id": "repo-a", "relationships": []any{},
		}, nil
	}}
	handler := &Handler{Neo4j: graph, Logger: logger, Instruments: instruments, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if !strings.Contains(logBuf.String(), "backend_anchor_mismatch") {
		t.Fatalf("log output = %q, want a backend_anchor_mismatch Warn", logBuf.String())
	}
	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "backend_anchor_mismatch"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), "entity_context"; got != want {
		t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestGetEntityContextGrantDeniedEmitsTelemetry is the ordinary-denial half
// of the same HTTP path: a scoped caller reading an entity whose hydrated
// repo_id is not in its grant must count reason=grant_denied.
func TestGetEntityContextGrantDeniedEmitsTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		return map[string]any{
			"id": "entity-a", "labels": []any{"Function"}, "name": "entity-a",
			"repo_id": "repo-out-of-grant", "relationships": []any{},
		}, nil
	}}
	handler := &Handler{Neo4j: graph, Instruments: instruments, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	}))
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "grant_denied"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestRecordScopedGrantDeniedSurvivesNilDependencies pins that the emitter is
// safe on the paths that have no telemetry wired: a Handler constructed
// without Instruments (most tests, and the local profile) must not panic.
func TestRecordScopedGrantDeniedSurvivesNilDependencies(t *testing.T) {
	t.Parallel()

	var h *Handler
	h.recordScopedGrantDenied(context.Background(), "workload_context", "grant_denied")
	(&Handler{}).recordScopedGrantDenied(context.Background(), "workload_context", "grant_denied")
	(&Handler{Instruments: &telemetry.Instruments{}}).recordScopedGrantDenied(context.Background(), "workload_context", "grant_denied")
}

// TestQueryScopedGrantDeniedOperationValues is the #6786 R3-2 review
// follow-up: it pins the remaining entries of
// telemetry.Instruments.QueryScopedGrantDenied's documented operation set
// that TestFetchWorkloadContextForOperationGrantDeniedEmitsTelemetry (which
// already covers "workload_context") and TestGetEntityContextGrantDeniedEmitsTelemetry
// (which already covers "entity_context") do not: "service_context",
// "service_story", and "service_investigation". Each is the literal
// operation string its real HTTP handler passes -- GetServiceContext
// (service_context_handler.go) calls fetchServiceWorkloadContext with
// "service_context" directly; GetServiceStory (service_story_handler.go) and
// InvestigateService (service_investigation.go) route through more
// indirection (BuildServiceStoryEnvelope /
// fetchServiceWorkloadContextWithSelector) before reaching
// FetchWorkloadContextForOperation with "service_story" /
// "service_investigation" respectively, so this test drives
// FetchWorkloadContextForOperation directly with those same literals rather
// than reconstructing that indirection: the claim under test is that
// QueryScopedGrantDenied's operation label is a verbatim passthrough of
// whatever operation string reaches FetchWorkloadContextForOperation, which
// this proves for all three, the same way the existing tests prove it for
// "workload_context".
func TestQueryScopedGrantDeniedOperationValues(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"service_context", "service_story", "service_investigation"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			instruments, reader := newTestInstruments(t)
			graph := graph.FakeGraphReader{
				RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
					return map[string]any{"id": "workload:out-of-grant", "name": "workload:out-of-grant", "repo_id": "repo-b"}, nil
				},
				RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
					return nil, nil // no DEFINES candidates: FetchWorkloadRepositoryForAccess resolves "".
				},
			}
			handler := &Handler{Neo4j: graph, Instruments: instruments}
			ctx := queryauth.ContextWithAuthContext(context.Background(), queryauth.AuthContext{
				Mode:                 queryauth.AuthModeScoped,
				AllowedRepositoryIDs: []string{"repo-a"},
			})

			got, err := handler.FetchWorkloadContextForOperation(
				ctx, "w.id = $service_name", map[string]any{"service_name": "workload:out-of-grant"}, operation,
			)
			if err != nil {
				t.Fatalf("FetchWorkloadContextForOperation() error = %v, want nil", err)
			}
			if got != nil {
				t.Fatalf("FetchWorkloadContextForOperation() = %#v, want nil", got)
			}

			points := scopedGrantDeniedDataPoints(t, reader)
			if len(points) != 1 {
				t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
			}
			if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), operation; got != want {
				t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
			}
		})
	}
}
