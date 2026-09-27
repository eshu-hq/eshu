// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestOCIRegistryTruthTruncationEmitsCounterByReason is decision test 6
// (#6590): the bounded OCI registry-truth read's eshu_dp_query_oci_registry_
// truth_truncated_total counter increments once per truncating statement
// shape, carrying only the bounded reason attribute -- mirroring
// TestImpactTraceK8sSelectWideningTruncationDisclosure
// (trace_deployment_k8s_select_truncation_test.go).
func TestOCIRegistryTruthTruncationEmitsCounterByReason(t *testing.T) {
	t.Run("tag_observation_row_limit", func(t *testing.T) {
		const refC = "ghcr.io/acme/c:latest"
		reader := ociBoundTestReader(t,
			map[string][]map[string]any{
				refC: repeatOCITagRow(refC, "sha256:"+strings.Repeat("1", 64), "repo:c", 750),
			},
			nil,
			nil,
		)
		instruments, reader2 := ociTruncationTestInstruments(t)
		handler := &Handler{Neo4j: reader, Instruments: instruments}

		result, err := handler.FetchOCIImageRegistryTruth(t.Context(), []string{refC}, "payments-api")
		if err != nil {
			t.Fatalf("FetchOCIImageRegistryTruth() error = %v", err)
		}
		if len(result.TruncatedImageRefs) != 1 {
			t.Fatalf("TruncatedImageRefs = %#v, want 1 withheld ref", result.TruncatedImageRefs)
		}

		if got := ociTruncationCounterValue(t, reader2, ociReasonTagObservationRowLimit); got != 1 {
			t.Fatalf("counter[%s] = %d, want 1", ociReasonTagObservationRowLimit, got)
		}
		if got := ociTruncationCounterValue(t, reader2, ociReasonImageRowLimit); got != 0 {
			t.Fatalf("counter[%s] = %d, want 0 (ref C never reached the image fetch)", ociReasonImageRowLimit, got)
		}
	})

	t.Run("image_row_limit", func(t *testing.T) {
		digestE := "sha256:" + strings.Repeat("e", 64)
		refE := "ghcr.io/acme/x@" + digestE
		eRows := make([]map[string]any, 0, 750)
		for i := 0; i < 750; i++ {
			eRows = append(eRows, ociImageRow(digestE, "repo:e"))
		}
		reader := ociBoundTestReader(t, nil, map[string][]map[string]any{digestE: eRows}, nil)
		instruments, reader2 := ociTruncationTestInstruments(t)
		handler := &Handler{Neo4j: reader, Instruments: instruments}

		result, err := handler.FetchOCIImageRegistryTruth(t.Context(), []string{refE}, "payments-api")
		if err != nil {
			t.Fatalf("FetchOCIImageRegistryTruth() error = %v", err)
		}
		if len(result.TruncatedImageRefs) != 1 {
			t.Fatalf("TruncatedImageRefs = %#v, want 1 withheld ref", result.TruncatedImageRefs)
		}

		if got := ociTruncationCounterValue(t, reader2, ociReasonImageRowLimit); got != 1 {
			t.Fatalf("counter[%s] = %d, want 1", ociReasonImageRowLimit, got)
		}
		if got := ociTruncationCounterValue(t, reader2, ociReasonTagObservationRowLimit); got != 0 {
			t.Fatalf("counter[%s] = %d, want 0 (no tag refs in this request)", ociReasonTagObservationRowLimit, got)
		}
	})
}

func ociTruncationTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("meter provider shutdown: %v", err)
		}
	})
	instruments, err := telemetry.NewInstruments(provider.Meter("impact-oci-truncation-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// ociTruncationCounterValue reads the summed value of the
// eshu_dp_query_oci_registry_truth_truncated_total counter for the given
// bounded reason attribute, asserting the datapoint carries exactly that one
// low-cardinality attribute (never service_name or a ref).
func ociTruncationCounterValue(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_query_oci_registry_truth_truncated_total" {
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
					t.Fatalf("counter datapoint attributes = %d, want 1 (reason only)", point.Attributes.Len())
				}
				return point.Value
			}
		}
	}
	return 0
}
