// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

var _ supplychaincore.SupplyChainImpactStoredKeyRecorder = (*supplyChainImpactTx)(nil)

// storedKeyMissingTotal sums the stored-key-missing counter and the attribute
// count across its points.
func storedKeyMissingTotal(t *testing.T, rm metricdata.ResourceMetrics) (total int64, attributes int) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_stored_key_missing_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("stored key missing metric data type = %T, want Int64 sum", m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
				attributes += dp.Attributes.Len()
			}
		}
	}
	return total, attributes
}

// TestStoredKeyMissingCountsAtCommitOnly proves the count follows the supports
// rows' rule: held in the transaction, counted when the commit succeeds, dropped
// by a rollback or a failed commit, with no label.
func TestStoredKeyMissingCountsAtCommitOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instruments, reader := conflictTestInstruments(t)

	committed := &supplyChainImpactTx{tx: &conflictScriptTx{}, instruments: instruments}
	committed.RecordStoredKeyMissing(ctx, 2)
	committed.RecordStoredKeyMissing(ctx, 3)
	if total, _ := storedKeyMissingTotal(t, collectConflicts(t, reader)); total != 0 {
		t.Fatalf("before commit the counter reads %d, want 0 (held)", total)
	}
	if err := committed.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if total, attrs := storedKeyMissingTotal(t, collectConflicts(t, reader)); total != 5 || attrs != 0 {
		t.Fatalf("after commit the counter reads %d with %d attributes, want 5 with none", total, attrs)
	}

	rolled := &supplyChainImpactTx{tx: &conflictScriptTx{}, instruments: instruments}
	rolled.RecordStoredKeyMissing(ctx, 7)
	if err := rolled.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	failed := &supplyChainImpactTx{tx: &conflictScriptTx{commitErr: errors.New("commit failed")}, instruments: instruments}
	failed.RecordStoredKeyMissing(ctx, 11)
	if err := failed.Commit(); err == nil {
		t.Fatal("Commit() error = nil, want the scripted failure")
	}
	if total, _ := storedKeyMissingTotal(t, collectConflicts(t, reader)); total != 5 {
		t.Fatalf("a rolled-back or failed pass moved the counter to %d, want it flat at 5", total)
	}

	// Zero rows and a transaction without instruments hold and count nothing.
	committed.RecordStoredKeyMissing(ctx, 0)
	(&supplyChainImpactTx{tx: &conflictScriptTx{}}).RecordStoredKeyMissing(ctx, 4)
}
