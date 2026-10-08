// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordZombieHealEmitsClosedOutcomes pins the heal counter's wire
// contract: every outcome label, including error, emits exactly, and nil
// instruments stay silent.
func TestRecordZombieHealEmitsClosedOutcomes(t *testing.T) {
	instruments, reader := newEnqueueInstruments(t)
	ctx := context.Background()
	for _, outcome := range []string{
		zombieHealOutcomeHealed,
		zombieHealOutcomeSkippedInFlight,
		zombieHealOutcomeSkippedAlreadyOpen,
		zombieHealOutcomeSkippedNoMarker,
		zombieHealOutcomeSkippedNoActive,
		zombieHealOutcomeError,
	} {
		recordZombieHeal(ctx, instruments, outcome)
	}
	recordZombieHeal(ctx, nil, zombieHealOutcomeHealed)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	counts := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_projector_zombie_heal_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				for _, attr := range dp.Attributes.ToSlice() {
					if string(attr.Key) == telemetry.MetricDimensionOutcome {
						counts[attr.Value.AsString()] += dp.Value
					}
				}
			}
		}
	}
	for _, outcome := range []string{
		zombieHealOutcomeHealed,
		zombieHealOutcomeSkippedInFlight,
		zombieHealOutcomeSkippedAlreadyOpen,
		zombieHealOutcomeSkippedNoMarker,
		zombieHealOutcomeSkippedNoActive,
		zombieHealOutcomeError,
	} {
		if counts[outcome] != 1 {
			t.Fatalf("outcome %q count = %d, want 1 (all: %v)", outcome, counts[outcome], counts)
		}
	}
}
