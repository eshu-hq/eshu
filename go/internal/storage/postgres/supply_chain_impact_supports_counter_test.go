// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func supportsRowsByActionAndSource(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_supply_chain_impact_supports_rows_total" {
				continue
			}
			for _, dp := range sum.DataPoints {
				action, _ := dp.Attributes.Value("action")
				source, _ := dp.Attributes.Value("source")
				got[source.AsString()+"/"+action.AsString()] += dp.Value
			}
		}
	}
	return got
}

// TestSupplyChainImpactTxCountsSupportRowsOnlyAfterCommit pins that the support
// counter, like the capture counter, moves only when the writer's transaction
// commits: a rollback and a failed commit add nothing, so the counter matches
// the rows that exist.
func TestSupplyChainImpactTxCountsSupportRowsOnlyAfterCommit(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		end       func(tx supplychaincore.SupplyChainImpactTx) error
		commitErr error
		want      map[string]int64
	}{
		"commit counts": {
			end:  func(tx supplychaincore.SupplyChainImpactTx) error { return tx.Commit() },
			want: map[string]int64{"writer/inserted": 3, "writer/updated": 1, "writer/unchanged": 4, "writer/deleted": 2},
		},
		"rollback counts nothing": {
			end:  func(tx supplychaincore.SupplyChainImpactTx) error { return tx.Rollback() },
			want: map[string]int64{},
		},
		"failed commit counts nothing": {
			end: func(tx supplychaincore.SupplyChainImpactTx) error {
				if tx.Commit() == nil {
					return errors.New("Commit() error = nil, want the commit failure")
				}
				return nil
			},
			commitErr: errors.New("commit failed"),
			want:      map[string]int64{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			beginner := SupplyChainImpactBeginner{Beginner: captureProbeBeginner{tx: &captureProbeTx{commitErr: tc.commitErr}}, Instruments: inst}
			tx, err := beginner.BeginSupplyChainImpactTx(context.Background())
			if err != nil {
				t.Fatalf("BeginSupplyChainImpactTx() error = %v", err)
			}
			impact := tx.(*supplyChainImpactTx)
			impact.holdSupportsRows(context.Background(), telemetry.SupplyChainImpactSupportsActionInserted, 3)
			impact.holdSupportsRows(context.Background(), telemetry.SupplyChainImpactSupportsActionUpdated, 1)
			impact.holdSupportsRows(context.Background(), telemetry.SupplyChainImpactSupportsActionUnchanged, 4)
			// The probe's Exec reports 2 affected rows: the retraction delete counts them.
			if removed, err := impact.DeleteRetractedImpactSupports(context.Background(), "scope", "generation"); err != nil || removed != 2 {
				t.Fatalf("DeleteRetractedImpactSupports() = %d, %v, want 2", removed, err)
			}
			if got := supportsRowsByActionAndSource(t, reader); len(got) != 0 {
				t.Fatalf("support rows before the transaction ended = %v, want none", got)
			}
			if err := tc.end(tx); err != nil {
				t.Fatal(err)
			}
			if got := supportsRowsByActionAndSource(t, reader); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("support rows = %v, want %v", got, tc.want)
			}
		})
	}
}
