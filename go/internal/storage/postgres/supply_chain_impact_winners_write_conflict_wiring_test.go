// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// conflictScriptTx scripts statement and commit failures for the conflict
// hook firing tests: a 40P01 (or other scripted error) proves each hook
// records exactly one classified point, and a non-conflict error proves the
// counter stays flat.
type conflictScriptTx struct {
	execErr   error
	commitErr error
}

type conflictFakeResult struct{}

func (conflictFakeResult) LastInsertId() (int64, error) { return 0, nil }
func (conflictFakeResult) RowsAffected() (int64, error) { return 1, nil }

func (tx *conflictScriptTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	if tx.execErr != nil {
		return nil, tx.execErr
	}
	return conflictFakeResult{}, nil
}

func (tx *conflictScriptTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("conflictScriptTx: no queries scripted")
}

func (tx *conflictScriptTx) Commit() error   { return tx.commitErr }
func (tx *conflictScriptTx) Rollback() error { return nil }

// conflictCounterValue sums the write-conflicts counter for one site and
// failure class from a manual reader.
func conflictCounterValue(t *testing.T, rm metricdata.ResourceMetrics, site, class string) int64 {
	t.Helper()
	var total int64
	var found bool
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_winners_write_conflicts_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("conflicts metric data type = %T, want Int64 sum", m.Data)
			}
			for _, dp := range sum.DataPoints {
				s, _ := dp.Attributes.Value(telemetry.MetricDimensionSite)
				c, _ := dp.Attributes.Value(telemetry.MetricDimensionFailureClass)
				if s.AsString() == site && c.AsString() == class {
					total += dp.Value
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatalf("no conflicts point for site=%q class=%q", site, class)
	}
	return total
}

func conflictTestInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return instruments, reader
}

func conflictTestID() supplychaincore.WinnersDirtyIdentifier {
	return supplychaincore.WinnersDirtyIdentifier{
		PackageID:     "pkg:npm/conflict-probe",
		RepositoryID:  "repo:conflict",
		SubjectDigest: "digest:conflict",
		AdvisoryID:    "CVE-2026-0002",
	}
}

func collectConflicts(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	return rm
}

// TestWinnersWriteConflictCaptureHookFires proves the writer capture hook
// records one deadlock point when the staging INSERT aborts with 40P01, and
// nothing for a non-conflict error: deleting the hook would pass every other
// non-live test in this package.
func TestWinnersWriteConflictCaptureHookFires(t *testing.T) {
	t.Parallel()

	instruments, reader := conflictTestInstruments(t)
	deadlock := &pgconn.PgError{Code: "40P01"}
	wtx := &supplyChainImpactTx{tx: &conflictScriptTx{execErr: deadlock}, instruments: instruments}
	if _, err := wtx.CaptureWinnersDirtyFacts(context.Background(), []supplychaincore.WinnersDirtyIdentifier{conflictTestID()}); err == nil {
		t.Fatal("CaptureWinnersDirtyFacts with 40P01 = nil, want the deadlock")
	}
	if got := conflictCounterValue(t, collectConflicts(t, reader), "writer_capture", "deadlock"); got != 1 {
		t.Fatalf("writer_capture/deadlock = %d, want exactly 1", got)
	}

	plain := &supplyChainImpactTx{tx: &conflictScriptTx{execErr: errors.New("boom")}, instruments: instruments}
	if _, err := plain.CaptureWinnersDirtyFacts(context.Background(), []supplychaincore.WinnersDirtyIdentifier{conflictTestID()}); err == nil {
		t.Fatal("CaptureWinnersDirtyFacts with plain error = nil, want the error")
	}
	// The failed plain write must not move the deadlock point.
	if got := conflictCounterValue(t, collectConflicts(t, reader), "writer_capture", "deadlock"); got != 1 {
		t.Fatalf("writer_capture/deadlock after plain error = %d, want still 1", got)
	}
}

// TestWinnersWriteConflictCommitHookFires proves the writer Commit hook
// records one deadlock point when Commit aborts with 40P01.
func TestWinnersWriteConflictCommitHookFires(t *testing.T) {
	t.Parallel()

	instruments, reader := conflictTestInstruments(t)
	deadlock := &pgconn.PgError{Code: "40P01"}
	wtx := &supplyChainImpactTx{tx: &conflictScriptTx{commitErr: deadlock}, instruments: instruments}
	if err := wtx.Commit(); err == nil {
		t.Fatal("Commit with 40P01 = nil, want the deadlock")
	}
	if got := conflictCounterValue(t, collectConflicts(t, reader), "writer_commit", "deadlock"); got != 1 {
		t.Fatalf("writer_commit/deadlock = %d, want exactly 1", got)
	}
}

// TestWinnersWriteConflictTickHookFires proves the tick phase hook records
// one serialization_failure point when a phase fails with 40001, and
// nothing for a non-conflict phase error.
func TestWinnersWriteConflictTickHookFires(t *testing.T) {
	t.Parallel()

	instruments, reader := conflictTestInstruments(t)
	store := SupplyChainImpactWinnersStore{}.WithInstruments(instruments)
	serErr := &pgconn.PgError{Code: "40001"}
	if err := store.timePhase(context.Background(), "collapse", func() error { return serErr }); err == nil {
		t.Fatal("timePhase with 40001 = nil, want the error")
	}
	if got := conflictCounterValue(t, collectConflicts(t, reader), "tick", "serialization_failure"); got != 1 {
		t.Fatalf("tick/serialization_failure = %d, want exactly 1", got)
	}
	if err := store.timePhase(context.Background(), "collapse", func() error { return errors.New("boom") }); err == nil {
		t.Fatal("timePhase with plain error = nil, want the error")
	}
	if got := conflictCounterValue(t, collectConflicts(t, reader), "tick", "serialization_failure"); got != 1 {
		t.Fatalf("tick/serialization_failure after plain error = %d, want still 1", got)
	}
}
