// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Shared metric-assertion helpers for bootstrap_telemetry_test.go. Split out
// (#4271) to keep bootstrap_telemetry_test.go under the repo's 500-line file
// cap as new phase-signal regression tests are added.

var errInjectedIaCFailure = errInjected("injected iac reachability failure")

type errInjected string

func (e errInjected) Error() string { return string(e) }

// contentEntityEmittedValue returns the counter value for
// eshu_dp_content_entity_emitted_total at the given source_file_kind under the
// bootstrap-index collector_kind.
func contentEntityEmittedValue(t *testing.T, rm metricdata.ResourceMetrics, kind string) int64 {
	t.Helper()
	const name = "eshu_dp_content_entity_emitted_total"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Sum[int64]", name, m.Data)
			}
			for _, p := range sum.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{
					telemetry.MetricDimensionSourceFileKind: kind,
					telemetry.MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return p.Value
				}
			}
		}
	}
	t.Fatalf("metric %s for source_file_kind %q not found", name, kind)
	return 0
}

// bootstrapPhaseRecorded reports whether a histogram data point exists for the
// named phase on eshu_dp_bootstrap_pipeline_phase_seconds.
func bootstrapPhaseRecorded(t *testing.T, rm metricdata.ResourceMetrics, phase string) bool {
	t.Helper()
	const name = "eshu_dp_bootstrap_pipeline_phase_seconds"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			for _, p := range hist.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{
					telemetry.MetricDimensionBootstrapPhase: phase,
					telemetry.MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return true
				}
			}
		}
	}
	return false
}

// bootstrapPhaseDurationSeconds returns the recorded histogram Sum (total
// seconds) for the named phase. A single Record per run means Sum equals that
// run's measured duration, which lets a test assert relative phase magnitudes.
func bootstrapPhaseDurationSeconds(t *testing.T, rm metricdata.ResourceMetrics, phase string) float64 {
	t.Helper()
	const name = "eshu_dp_bootstrap_pipeline_phase_seconds"
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Histogram[float64]", name, m.Data)
			}
			for _, p := range hist.DataPoints {
				if metricPointHasAttrs(p.Attributes, map[string]string{
					telemetry.MetricDimensionBootstrapPhase: phase,
					telemetry.MetricDimensionCollectorKind:  "bootstrap-index",
				}) {
					return p.Sum
				}
			}
		}
	}
	t.Fatalf("metric %s for phase %q not found", name, phase)
	return 0
}

// metricPointHasAttrs reports whether every want key/value pair is present in
// the data point attribute set.
func metricPointHasAttrs(set attribute.Set, want map[string]string) bool {
	for k, v := range want {
		val, ok := set.Value(attribute.Key(k))
		if !ok || val.AsString() != v {
			return false
		}
	}
	return true
}
