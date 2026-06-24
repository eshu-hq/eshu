// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchdecaytelemetry

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdecay"
	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestObserverRecordsPolicyClassOutcomeCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("search-decay-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	_, err = searchdecay.Scorer{
		Policy: searchdecay.Policy{
			ID:       "decay-v1",
			Now:      now,
			HalfLife: 24 * time.Hour,
			MinScore: 0.1,
		},
		Observer: NewObserver(inst),
	}.Score(context.Background(), searchdecay.Evidence{
		ID:         "vuln:obs-1",
		Class:      searchdecay.EvidenceClassVulnerabilityObservation,
		TruthLevel: searchdocs.TruthLevelDerived,
		ObservedAt: now.Add(-48 * time.Hour),
		Score:      0.8,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValue(t, rm, "eshu_dp_search_decay_policy_applications_total", map[string]string{
		"policy_id":      "decay-v1",
		"evidence_class": "vulnerability_observation",
		"outcome":        "applied",
	}, 1)
}

func TestObserverRecordsCanonicalSkipOutcomeCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("search-decay-skip-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	now := time.Date(2026, 6, 6, 10, 0, 0, 0, time.UTC)
	_, err = searchdecay.Scorer{
		Policy: searchdecay.Policy{
			ID:       "decay-v1",
			Now:      now,
			HalfLife: 24 * time.Hour,
			MinScore: 0.1,
		},
		Observer: NewObserver(inst),
	}.Score(context.Background(), searchdecay.Evidence{
		ID:         "service:checkout",
		Class:      searchdecay.EvidenceClassCanonicalGraph,
		TruthLevel: searchdocs.TruthLevel("canonical"),
		ObservedAt: now.Add(-720 * time.Hour),
		Score:      0.9,
	})
	if err != nil {
		t.Fatalf("Score() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertCounterValue(t, rm, "eshu_dp_search_decay_policy_applications_total", map[string]string{
		"policy_id":      "decay-v1",
		"evidence_class": "canonical_graph",
		"outcome":        "skipped_canonical",
	}, 1)
}

func assertCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
	want int64,
) {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if metricAttrsMatch(point.Attributes.ToSlice(), wantAttrs) {
					if point.Value != want {
						t.Fatalf("%s value = %d, want %d", metricName, point.Value, want)
					}
					return
				}
			}
		}
	}
	t.Fatalf("metric %q with attrs %#v not found", metricName, wantAttrs)
}

func metricAttrsMatch(attrs []attribute.KeyValue, want map[string]string) bool {
	if len(attrs) != len(want) {
		return false
	}
	for _, attr := range attrs {
		wantValue, ok := want[string(attr.Key)]
		if !ok || attr.Value.AsString() != wantValue {
			return false
		}
	}
	return true
}
