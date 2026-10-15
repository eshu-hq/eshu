// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestServiceCatalogRuntimeReadinessCounterRecordsEveryOutcome pins the closed
// outcome set of eshu_dp_service_catalog_runtime_readiness_total: one count
// per fenced pass, labeled by the decision the fence took.
func TestServiceCatalogRuntimeReadinessCounterRecordsEveryOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		outcome    string
		states     map[string]RuntimeProducerState
		startedAgo time.Duration
	}{
		{outcome: RuntimeReadinessReady, states: map[string]RuntimeProducerState{"repo-checkout": {Total: 1}}, startedAgo: time.Minute},
		{outcome: RuntimeReadinessNoProducer, states: nil, startedAgo: time.Minute},
		{outcome: RuntimeReadinessDeferred, states: map[string]RuntimeProducerState{"repo-checkout": {Running: 1, Total: 1}}, startedAgo: time.Minute},
		{outcome: RuntimeReadinessBoundExceeded, states: map[string]RuntimeProducerState{"repo-checkout": {Pending: 1, Total: 1}}, startedAgo: crossscope.ProducerReadinessMaxWait + time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			t.Parallel()
			inst, reader := newServiceCatalogCorrelationInstruments(t)
			h := newRuntimeFenceHarness(tc.states, []ServiceRuntimeInstance{runtimeFenceInstance})
			h.handler.Instruments = inst
			_, _ = h.handler.Handle(context.Background(), runtimeFenceIntent(tc.startedAgo))

			var rm metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			got := reducerCounterValue(t, rm, "eshu_dp_service_catalog_runtime_readiness_total", map[string]string{
				"domain":  "service_catalog_correlation",
				"outcome": tc.outcome,
			})
			if got != 1 {
				t.Fatalf("eshu_dp_service_catalog_runtime_readiness_total{outcome=%q} = %d, want 1", tc.outcome, got)
			}
		})
	}
}

// TestServiceCatalogRuntimeFenceLogsDeferralWithCountsOnly pins the operator
// log line: the deferral goes to the wired logger with the in-flight
// repository, producer state counts, elapsed time, and the bound, and never
// attempt_count, which the non-counting class freezes.
func TestServiceCatalogRuntimeFenceLogsDeferralWithCountsOnly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := newRuntimeFenceHarness(map[string]RuntimeProducerState{"repo-checkout": {Claimed: 1, Pending: 2, Total: 3}}, nil)
	h.handler.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	if _, err := h.handler.Handle(context.Background(), runtimeFenceIntent(time.Minute)); err == nil {
		t.Fatal("Handle() error = nil, want a deferral")
	}
	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &line); err != nil {
		t.Fatalf("decode log line %q: %v", buf.String(), err)
	}
	for key, want := range map[string]any{
		"runtime_readiness_outcome": RuntimeReadinessDeferred,
		"failure_class":             RuntimeNotReadyFailureClass,
		"in_flight_repo_count":      float64(1),
		"producer_pending":          float64(2),
		"producer_claimed":          float64(1),
		"elapsed_since_cycle_start": float64(time.Minute),
		"max_wait":                  float64(crossscope.ProducerReadinessMaxWait),
	} {
		if got := line[key]; got != want {
			t.Errorf("log %s = %#v, want %#v", key, got, want)
		}
	}
	if ids, _ := line["in_flight_repo_ids"].([]any); len(ids) != 1 || ids[0] != "repo-checkout" {
		t.Errorf("log in_flight_repo_ids = %#v, want [repo-checkout]", line["in_flight_repo_ids"])
	}
	if _, ok := line["attempt_count"]; ok {
		t.Error("deferral log carries attempt_count, which the non-counting class freezes")
	}
}
