// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestResolveWorkloadSelectorGrantCapEmitsInlineCappedTelemetry covers #6801
// review F-R6-2 for the deployment-trace selector. A token over the 128-term
// SHAPE-A cap emits the #5408 cap signal with reason
// deployment_trace_selector; a token under it emits nothing.
func TestResolveWorkloadSelectorGrantCapEmitsInlineCappedTelemetry(t *testing.T) {
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
			ctx := auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
				Mode: auth.AuthModeScoped, AllowedRepositoryIDs: tc.grants,
			})
			if _, err := ResolveWorkloadSelector(ctx, graph.FakeGraphReader{}, "api", nil, instruments); err != nil {
				t.Fatalf("ResolveWorkloadSelector() error = %v", err)
			}
			var collected metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &collected); err != nil {
				t.Fatalf("collect metrics: %v", err)
			}
			var reasons []string
			for _, scope := range collected.ScopeMetrics {
				for _, m := range scope.Metrics {
					if m.Name != "eshu_dp_query_scope_grant_inline_capped_total" {
						continue
					}
					for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
						v, _ := dp.Attributes.Value(attribute.Key("reason"))
						reasons = append(reasons, v.AsString())
					}
				}
			}
			if len(reasons) != tc.want {
				t.Fatalf("capped data points = %v, want %d", reasons, tc.want)
			}
			if tc.want == 1 && reasons[0] != "deployment_trace_selector" {
				t.Fatalf("reason = %q, want deployment_trace_selector", reasons[0])
			}
		})
	}
}
