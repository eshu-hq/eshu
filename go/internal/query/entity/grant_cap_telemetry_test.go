// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

const scopeGrantInlineCappedMetric = "eshu_dp_query_scope_grant_inline_capped_total"

// cappedReasons returns the reason label of every data point on the
// SHAPE-A inline-cap counter.
func cappedReasons(t *testing.T, reader *sdkmetric.ManualReader) []string {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var reasons []string
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != scopeGrantInlineCappedMetric {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				v, _ := dp.Attributes.Value(attribute.Key("reason"))
				reasons = append(reasons, v.AsString())
			}
		}
	}
	return reasons
}

// TestGetServiceContextGrantCapEmitsInlineCappedTelemetry covers #6801 review
// F-R6-2. Past the 128-term SHAPE-A cap, the scoped name read drops the
// overflow grants' DEFINES terms and fails closed. The read must emit the
// #5408 cap signal, so an operator can see why a DEFINES-only workload went
// missing. A token under the cap emits nothing.
func TestGetServiceContextGrantCapEmitsInlineCappedTelemetry(t *testing.T) {
	t.Parallel()

	grants := make([]string, 129)
	for i := range grants {
		grants[i] = fmt.Sprintf("repo-%03d", i)
	}
	for _, tc := range []struct {
		name   string
		grants []string
		want   int
	}{
		{"over_cap", grants, 1},
		{"under_cap", grants[:2], 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instruments, reader := newTestInstruments(t)
			handler := &Handler{Neo4j: graph.FakeGraphReader{}, Instruments: instruments}
			ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
				Mode: auth.AuthModeScoped, AllowedRepositoryIDs: tc.grants,
			})
			req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/context", nil).WithContext(ctx)
			req.SetPathValue("service_name", "api")
			handler.GetServiceContext(httptest.NewRecorder(), req)

			reasons := cappedReasons(t, reader)
			if len(reasons) != tc.want {
				t.Fatalf("%s data points = %v, want %d", scopeGrantInlineCappedMetric, reasons, tc.want)
			}
			if tc.want == 1 && reasons[0] != "workload_context_name" {
				t.Fatalf("reason = %q, want workload_context_name", reasons[0])
			}
		})
	}
}
