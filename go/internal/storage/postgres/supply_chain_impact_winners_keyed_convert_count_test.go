// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestSettleWinnersKeyedDirtyLogReportsConvertedRows pins the #11145 count
// wiring through the settle: the convert's RowsAffected reaches the caller's
// out-param, a driver that cannot report affected rows yields 0 with no error
// (the count is telemetry; the conversion already succeeded), and the
// over-cap fallback, which converts nothing, leaves the count at 0.
func TestSettleWinnersKeyedDirtyLogReportsConvertedRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	reported := &settleFakeTx{drained: [][2]any{{"activation", int64(1)}}, converted: 7}
	var converted int64
	if _, _, _, _, err := settleWinnersKeyedDirtyLog(ctx, reported, false, &converted); err != nil {
		t.Fatalf("settle with a reported convert count: %v", err)
	}
	if converted != 7 {
		t.Errorf("converted = %d, want the convert's RowsAffected 7", converted)
	}

	unreported := &settleFakeTx{
		drained: [][2]any{{"activation", int64(1)}}, converted: 7,
		convertErr: errors.New("driver cannot report affected rows"),
	}
	converted = -1
	if _, _, _, _, err := settleWinnersKeyedDirtyLog(ctx, unreported, false, &converted); err != nil {
		t.Fatalf("settle with an unreportable count: %v, want no error", err)
	}
	if converted != 0 {
		t.Errorf("converted = %d with a RowsAffected error, want 0", converted)
	}

	fallback := &settleFakeTx{drained: [][2]any{{"activation", int64(1)}}, converted: 7}
	converted = 0
	if _, _, _, _, err := settleWinnersKeyedDirtyLog(ctx, fallback, true, &converted); err != nil {
		t.Fatalf("settle over-cap: %v", err)
	}
	if converted != 0 {
		t.Errorf("over-cap converted = %d, want 0 (the fallback runs no convert)", converted)
	}
}

// TestResweepWinnersKeyedRecordsScopeRemainderConverted pins the tick end of
// the #11145 signal: a keyed tick whose convert wrote N rows reports N on
// the result and adds N to
// eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total.
func TestResweepWinnersKeyedRecordsScopeRemainderConverted(t *testing.T) {
	t.Parallel()

	tx := phaseFakeTx{
		claimed: true, consumed: 3, identifier: 3, stored: 100,
		affected: map[string]int64{convertWinnersKeyedScopeRemainderSQL: 4},
	}
	store, reader := newPhaseStore(t, &tx)
	result, err := store.ResweepWinnersKeyed(context.Background())
	if err != nil {
		t.Fatalf("ResweepWinnersKeyed() error = %v", err)
	}
	if result.ScopeRemainderConverted != 4 {
		t.Errorf("ScopeRemainderConverted = %d, want 4", result.ScopeRemainderConverted)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_winners_scope_remainder_converted_rows_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("converted metric data type = %T, want Int64 sum", m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	if total != 4 {
		t.Errorf("scope_remainder_converted_rows_total = %d, want 4", total)
	}
}
