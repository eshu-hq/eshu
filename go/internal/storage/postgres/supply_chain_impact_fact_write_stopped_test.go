// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stoppedWriteErr is the error the PR-C reject trigger raises: SQLSTATE 55000
// (object_not_in_prerequisite_state) with the trigger's constraint name.
func stoppedWriteErr() *pgconn.PgError {
	return &pgconn.PgError{
		Code:           "55000",
		ConstraintName: supplyChainImpactFactWriteRejectConstraint,
		Message:        "supply chain impact finding facts are no longer written",
	}
}

// TestSupplyChainImpactFactWriteStoppedClassification pins which errors the
// adapter takes as the write stop: SQLSTATE 55000 AND the trigger's constraint
// name, nothing else. Another 55000 (a locked-out object, an unfinished index)
// and the trigger's constraint under another code must keep their ordinary
// terminal classification, so the new non-counting class never hides a defect.
func TestSupplyChainImpactFactWriteStoppedClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"trigger error", stoppedWriteErr(), true},
		{"trigger error wrapped", fmt.Errorf("write supply chain impact fact: %w", stoppedWriteErr()), true},
		{"other 55000", &pgconn.PgError{Code: "55000", ConstraintName: "some_other_constraint"}, false},
		{"55000 without constraint", &pgconn.PgError{Code: "55000"}, false},
		{"trigger constraint under another code", &pgconn.PgError{Code: "23514", ConstraintName: supplyChainImpactFactWriteRejectConstraint}, false},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, false},
		{"plain error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifySupplyChainImpactFactWriteStopped(tc.err)
			var stopped *SupplyChainImpactFactWriteStoppedError
			isStopped := errors.As(got, &stopped)
			if isStopped != tc.want {
				t.Fatalf("classify(%v) stopped = %v, want %v", tc.err, isStopped, tc.want)
			}
			if !tc.want {
				if got != tc.err {
					t.Fatalf("classify(%v) = %v, want the error unchanged", tc.err, got)
				}
				if reducercontract.IsRetryable(got) {
					t.Fatalf("an unclassified error %v is retryable, want terminal", got)
				}
				return
			}
			if !reducercontract.IsRetryable(got) {
				t.Fatal("stopped write is not retryable, want retryable")
			}
			if reducercontract.IsExpectedWait(got) {
				t.Fatal("stopped write is an expected wait, want a non-counting retry that the service defers")
			}
			if class := stopped.FailureClass(); class != "supply_chain_impact_fact_write_stopped" {
				t.Fatalf("FailureClass() = %q, want supply_chain_impact_fact_write_stopped", class)
			}
			var pgErr *pgconn.PgError
			if !errors.As(got, &pgErr) || pgErr.Code != "55000" {
				t.Fatalf("classified error lost the underlying 55000: %v", got)
			}
		})
	}
}

// TestSupplyChainImpactFactWriteStoppedIsNonCounting proves the class is in the
// reducer queue's non-counting set and the waiting map. A class missing from
// either counts toward the attempt budget and dead-letters an intent that only
// ran against an old image during a roll.
func TestSupplyChainImpactFactWriteStoppedIsNonCounting(t *testing.T) {
	t.Parallel()

	const class = "supply_chain_impact_fact_write_stopped"
	if SupplyChainImpactFactWriteStoppedFailureClass != class {
		t.Fatalf("SupplyChainImpactFactWriteStoppedFailureClass = %q, want %q", SupplyChainImpactFactWriteStoppedFailureClass, class)
	}
	if !IsNonCountingReducerRetryFailureClass(class) {
		t.Fatalf("%s is not in the non-counting reducer retry set", class)
	}
	entry, ok := WaitingDeferralByClass()[class]
	if !ok {
		t.Fatalf("%s has no WaitingDeferralMap entry", class)
	}
	if entry.WakeSite != WaitingWakeSiteNotAWait {
		t.Fatalf("%s wake site = %q, want not-a-wait (the retry timer is the only wake)", class, entry.WakeSite)
	}
}

// stoppedScriptTx scripts one failing write or read on the impact transaction.
type stoppedScriptTx struct {
	execErr  error
	queryErr error
}

func (tx *stoppedScriptTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	if tx.execErr != nil {
		return nil, tx.execErr
	}
	return conflictFakeResult{}, nil
}

func (tx *stoppedScriptTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	return nil, errors.New("stoppedScriptTx: no rows scripted")
}

func (tx *stoppedScriptTx) Commit() error   { return nil }
func (tx *stoppedScriptTx) Rollback() error { return nil }

// rejectedCounterValue returns the rejected counter's total and the attribute
// count across its points, so a test can pin both once-per-rejection and the
// no-label contract.
func rejectedCounterValue(t *testing.T, rm metricdata.ResourceMetrics) (total int64, attributes int) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_fact_write_rejected_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("rejected metric data type = %T, want Int64 sum", m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
				attributes += dp.Attributes.Len()
			}
		}
	}
	return total, attributes
}

// TestSupplyChainImpactTxCountsOneRejectionPerStoppedWrite proves the writer
// transaction classifies the stop on both statement paths (Exec for the insert
// and the tombstone UPDATE, Query for a write that returns rows), counts exactly
// one per rejection with no label (no scope or generation id can reach a
// metric), and counts nothing for any other error.
func TestSupplyChainImpactTxCountsOneRejectionPerStoppedWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instruments, reader := conflictTestInstruments(t)
	tx := &supplyChainImpactTx{tx: &stoppedScriptTx{execErr: stoppedWriteErr(), queryErr: stoppedWriteErr()}, instruments: instruments}

	_, err := tx.ExecContext(ctx, "INSERT INTO fact_records ...")
	var stopped *SupplyChainImpactFactWriteStoppedError
	if !errors.As(err, &stopped) {
		t.Fatalf("ExecContext error = %v, want the stopped-write error", err)
	}
	if total, attrs := rejectedCounterValue(t, collectConflicts(t, reader)); total != 1 || attrs != 0 {
		t.Fatalf("after one rejected exec: total = %d with %d attributes, want 1 with none", total, attrs)
	}

	if _, err := tx.QueryContext(ctx, "INSERT INTO fact_records ... RETURNING fact_id"); !errors.As(err, &stopped) {
		t.Fatalf("QueryContext error = %v, want the stopped-write error", err)
	}
	if total, _ := rejectedCounterValue(t, collectConflicts(t, reader)); total != 2 {
		t.Fatalf("after a second rejection on the query path: total = %d, want 2", total)
	}

	other := &supplyChainImpactTx{
		tx:          &stoppedScriptTx{execErr: &pgconn.PgError{Code: "55000", ConstraintName: "other"}},
		instruments: instruments,
	}
	if _, err := other.ExecContext(ctx, "UPDATE fact_records ..."); err == nil || errors.As(err, &stopped) {
		t.Fatalf("other 55000 error = %v, want it returned unclassified", err)
	}
	if total, _ := rejectedCounterValue(t, collectConflicts(t, reader)); total != 2 {
		t.Fatalf("an unrelated error moved the counter to %d, want it flat at 2", total)
	}

	// A transaction without instruments classifies and counts nothing.
	bare := &supplyChainImpactTx{tx: &stoppedScriptTx{execErr: stoppedWriteErr()}}
	if _, err := bare.ExecContext(ctx, "INSERT INTO fact_records ..."); !errors.As(err, &stopped) {
		t.Fatalf("instrument-less tx error = %v, want the stopped-write error", err)
	}
}

// TestSupplyChainImpactTxDoesNotCountAnAlreadyClassifiedError pins the "counted
// once" guard: an error that already carries the stopped-write classification
// passes through the transaction unchanged and does not move the counter, so a
// refusal counted by an inner layer is never counted again by an outer one.
func TestSupplyChainImpactTxDoesNotCountAnAlreadyClassifiedError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instruments, reader := conflictTestInstruments(t)
	already := &SupplyChainImpactFactWriteStoppedError{Err: stoppedWriteErr()}
	tx := &supplyChainImpactTx{tx: &stoppedScriptTx{execErr: already, queryErr: already}, instruments: instruments}

	_, err := tx.ExecContext(ctx, "INSERT INTO fact_records ...")
	var stopped *SupplyChainImpactFactWriteStoppedError
	if !errors.As(err, &stopped) || stopped != already {
		t.Fatalf("ExecContext error = %v, want the same classified error back", err)
	}
	if _, err := tx.QueryContext(ctx, "UPDATE fact_records ... RETURNING fact_id"); !errors.As(err, &stopped) || stopped != already {
		t.Fatalf("QueryContext error = %v, want the same classified error back", err)
	}
	if total, _ := rejectedCounterValue(t, collectConflicts(t, reader)); total != 0 {
		t.Fatalf("an already classified error moved the counter to %d, want it flat at 0", total)
	}
}
