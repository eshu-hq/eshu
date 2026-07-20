// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Test 8 (proof-matrix item 7) -- truncation disclosure. When the directed
// SELECTS candidate scan hits the repositorySemanticEntityLimit ceiling, the
// response reports k8s_relationships_complete=false with the machine-readable
// reason, and the low-cardinality truncation counter increments once carrying
// only the bounded reason attribute (never repo_id).
func TestImpactTraceK8sSelectWideningTruncationDisclosure(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("impact-k8s-select-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}

	// One anchored Deployment plus enough Service candidates to overflow the
	// sentinel (repositorySemanticEntityLimit + 1 rows returned by the scan).
	deployment := k8sEntity("dep-web", "web", "deploy/web.yaml", "Deployment", "prod", map[string]string{
		"pod_template_labels": "app=web,tier=api",
	})
	entities := []EntityContent{deployment}
	for i := range repositorySemanticEntityLimit + 1 {
		id := fmt.Sprintf("svc-%05d", i)
		entities = append(entities, k8sEntity(id, "noise-"+id, fmt.Sprintf("svc/%s.yaml", id), "Service", "prod", map[string]string{
			"selector": fmt.Sprintf("app=noise-%05d", i),
		}))
	}

	handler := &ImpactHandler{
		Content:     newK8sSelectWideningStore(entities),
		Instruments: instruments,
	}
	result, err := handler.fetchK8sResourceResult(context.Background(), "repo-1", "web")
	if err != nil {
		t.Fatalf("fetchK8sResourceResult() error = %v", err)
	}

	if !result.selectCandidatePoolTruncated {
		t.Fatalf("selectCandidatePoolTruncated = false, want true at the sentinel")
	}
	if got := BoolVal(result.limits, "k8s_relationships_complete"); got {
		t.Fatalf("k8s_relationships_complete = true, want false at the sentinel")
	}
	if got, want := StringVal(result.limits, "k8s_relationships_incomplete_reason"), k8sSelectCandidatePoolTruncationReason; got != want {
		t.Fatalf("k8s_relationships_incomplete_reason = %q, want %q", got, want)
	}

	got := candidatePoolTruncationCounterValue(t, reader, k8sSelectCandidatePoolTruncationReason)
	if got != 1 {
		t.Fatalf("truncation counter = %d, want 1", got)
	}
}

// candidatePoolTruncationCounterValue reads the summed value of the
// eshu_dp_query_k8s_select_candidate_scan_truncated_total counter for the given
// bounded reason attribute, asserting the datapoint carries exactly that one
// low-cardinality attribute.
func candidatePoolTruncationCounterValue(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_query_k8s_select_candidate_scan_truncated_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data = %T, want Sum[int64]", record.Data)
			}
			for _, point := range sum.DataPoints {
				value, present := point.Attributes.Value("reason")
				if !present || value.AsString() != reason {
					continue
				}
				if point.Attributes.Len() != 1 {
					t.Fatalf("counter datapoint attributes = %d, want 1 (reason only, no repo_id)", point.Attributes.Len())
				}
				return point.Value
			}
		}
	}
	return 0
}
