// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const queryScopedGrantDeniedMetric = "eshu_dp_query_scoped_grant_denied_total"

// newTestInstruments builds a real OTEL Instruments backed by a manual
// reader, so these tests observe the actual wire-level counter (name,
// attribute keys) rather than a mock of it.
func newTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("impacttrace-scoped-grant-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// scopedGrantDeniedDataPoints collects every eshu_dp_query_scoped_grant_denied_total
// data point, so a test can assert on both count and the reason/operation
// labels rather than just "the counter moved".
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

// TestResolveWorkloadSelectorIDMismatchEmitsAnchorMismatchTelemetry is
// the #6786 R2-4 proof for the id-lookup F3 guard: a row whose own id
// disagrees with the selector must both Warn-log and count
// reason=backend_anchor_mismatch, the operator-visible signal for a NornicDB
// anchor regression, not merely fall through silently.
func TestResolveWorkloadSelectorIDMismatchEmitsAnchorMismatchTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	graph := querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{"id": "workload:different", "repo_id": "repo-a"}}, nil
		}
		return nil, nil
	}}

	got, err := ResolveWorkloadSelector(t.Context(), graph, "workload:requested", logger, instruments)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found", got)
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
	if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), "deployment_trace_selector"; got != want {
		t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestResolveWorkloadSelectorOutOfGrantIDEmitsGrantDeniedTelemetry is
// the ordinary-denial half: an exact id match that the grant does not admit
// must count reason=grant_denied (not backend_anchor_mismatch, and no Warn --
// this is expected scoped-caller behavior, not a backend regression signal).
func TestResolveWorkloadSelectorOutOfGrantIDEmitsGrantDeniedTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))

	graph := querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{"id": "workload:out-of-grant", "repo_id": "repo-b", "defining": []string{}}}, nil
		}
		return nil, nil
	}}

	got, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "workload:out-of-grant", logger, instruments)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("ResolveWorkloadSelector() = %q, want not-found", got)
	}

	if strings.Contains(logBuf.String(), "backend_anchor_mismatch") {
		t.Fatalf("log output = %q, want no anchor-mismatch Warn for an ordinary scoped denial", logBuf.String())
	}

	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "grant_denied"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}

// TestResolveWorkloadSelectorSurvivesNilTelemetry pins that both
// telemetry parameters are optional: a caller that has not wired the full
// stack (every other test in this package) must not panic.
func TestResolveWorkloadSelectorSurvivesNilTelemetry(t *testing.T) {
	t.Parallel()

	graph := querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{"id": "workload:different", "repo_id": "repo-a"}}, nil
		}
		return nil, nil
	}}

	if _, err := ResolveWorkloadSelector(t.Context(), graph, "workload:requested", nil, nil); err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
	if _, err := ResolveWorkloadSelector(t.Context(), graph, "workload:requested", nil, &telemetry.Instruments{}); err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}
}

// TestResolveWorkloadSelectorOperationLabel is the #6786 R3-2 review
// follow-up: it pins QueryScopedGrantDenied's operation label for this
// package's one emission seam at "deployment_trace_selector" --
// ResolveWorkloadSelector's own internal constant, NOT
// "resolve_trace_workload_selector" (the name the metric's doc comment
// incorrectly listed before this fix) and NOT "deployment_trace" (the
// distinct operation its caller separately passes to the follow-up
// FetchWorkloadContextForOperation call in the same request, see
// family_impact_trace_deployment.go). A rename of the internal constant
// without updating go/internal/telemetry/instruments.go's documented
// operation set fails this test.
func TestResolveWorkloadSelectorOperationLabel(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return []map[string]any{{"id": "workload:out-of-grant", "repo_id": "repo-b", "defining": []string{}}}, nil
		}
		return nil, nil
	}}

	_, err := ResolveWorkloadSelector(scopedAuthContext("repo-a"), graph, "workload:out-of-grant", nil, instruments)
	if err != nil {
		t.Fatalf("ResolveWorkloadSelector() error = %v, want nil", err)
	}

	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 {
		t.Fatalf("%s data points = %d, want 1: %+v", queryScopedGrantDeniedMetric, len(points), points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionOperation), "deployment_trace_selector"; got != want {
		t.Fatalf("%s operation = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}
