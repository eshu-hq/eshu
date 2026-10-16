// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package consumption

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// fakeScope scripts the statements a rebuild sends. Each Begin opens one
// transaction; lockErrAt fails the scope lock of that transaction number
// (1-based), and clearAffected is what the compare-and-delete reports.
type fakeScope struct {
	lockErrAt     int
	lockErr       error
	clearAffected int64
	begins        int
	commits       int
	rollbacks     int
	statements    []string
}

func (f *fakeScope) Begin(context.Context) (db.Transaction, error) {
	f.begins++
	return &fakeTx{scope: f, number: f.begins}, nil
}

type fakeTx struct {
	scope  *fakeScope
	number int
	done   bool
}

func (tx *fakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	tx.scope.statements = append(tx.scope.statements, query)
	switch query {
	case lockScopeSQL:
		if tx.number == tx.scope.lockErrAt {
			return nil, tx.scope.lockErr
		}
		return &fakeRows{values: []any{"scope-a"}}, nil
	case dirtyScopePresentSQL:
		return &fakeRows{values: []any{true}}, nil
	case fenceDirtyScopeSQL:
		return &fakeRows{values: []any{"4242"}}, nil
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
}

func (tx *fakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	tx.scope.statements = append(tx.scope.statements, query)
	if query == clearDirtyScopeIfUnchangedSQL {
		return fakeResult(tx.scope.clearAffected), nil
	}
	return fakeResult(1), nil
}

func (tx *fakeTx) Commit() error {
	tx.done = true
	tx.scope.commits++
	return nil
}

func (tx *fakeTx) Rollback() error {
	if !tx.done {
		tx.scope.rollbacks++
	}
	tx.done = true
	return nil
}

type fakeRows struct {
	values []any
	read   bool
}

func (r *fakeRows) Next() bool {
	if r.read || len(r.values) == 0 {
		return false
	}
	r.read = true
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	switch target := dest[0].(type) {
	case *string:
		*target = r.values[0].(string)
	case *bool:
		*target = r.values[0].(bool)
	default:
		return fmt.Errorf("unsupported scan target %T", dest[0])
	}
	return nil
}

func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }

type fakeResult int64

func (fakeResult) LastInsertId() (int64, error)   { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return int64(r), nil }

// pagedLoader serves full pages until total facts are handed out.
func pagedLoader(pageSize, total int) PageLoader {
	return func(_ context.Context, _ db.Queryer, scopeID, after string) ([]facts.Envelope, error) {
		start := 0
		if after != "" {
			if _, err := fmt.Sscanf(after, "fact-%06d", &start); err != nil {
				return nil, err
			}
			start++
		}
		var page []facts.Envelope
		for i := start; i < total && len(page) < pageSize; i++ {
			page = append(page, facts.Envelope{FactID: fmt.Sprintf("fact-%06d", i), ScopeID: scopeID})
		}
		return page, nil
	}
}

func newTestRebuilder(t *testing.T, scope *fakeScope, total int) (Rebuilder, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return Rebuilder{
		Beginner:    scope,
		LoadPage:    pagedLoader(3, total),
		Refresh:     func(context.Context, db.ExecQueryer, []facts.Envelope) error { return nil },
		PageSize:    3,
		Instruments: instruments,
	}, reader
}

func TestRebuildScopeCommitsOneTransactionPerPageAndClearsUnchangedFence(t *testing.T) {
	scope := &fakeScope{clearAffected: 1}
	rebuilder, reader := newTestRebuilder(t, scope, 7)

	result, err := rebuilder.RebuildScope(context.Background(), "scope-a", PhaseInitial)
	if err != nil {
		t.Fatalf("RebuildScope() error = %v", err)
	}
	if result.Outcome != OutcomeCleared || result.Pages != 3 || result.Facts != 7 || result.Transactions != 4 {
		t.Fatalf("RebuildScope() = %+v, want cleared, 3 pages, 7 facts, 4 transactions", result)
	}
	if scope.begins != 4 || scope.commits != 4 || scope.rollbacks != 0 {
		t.Fatalf("transactions begun/committed/rolled back = %d/%d/%d, want 4/4/0", scope.begins, scope.commits, scope.rollbacks)
	}
	metrics := collectMetrics(t, reader)
	if got := metrics["eshu_dp_package_manifest_backfill_page_duration_seconds|outcome=committed"]; got != 4 {
		t.Fatalf("committed transaction observations = %d, want 4", got)
	}
	if got := metrics["eshu_dp_package_manifest_backfill_scope_lock_wait_seconds|"]; got != 4 {
		t.Fatalf("scope lock wait observations = %d, want 4", got)
	}
	if got := metrics["eshu_dp_package_manifest_backfill_scope_rebuilds_total|outcome=cleared,phase=initial"]; got != 1 {
		t.Fatalf("cleared initial rebuilds = %d, want 1 (metrics %v)", got, metrics)
	}
}

func TestRebuildScopeLeavesFenceWhenDirtyRowChanged(t *testing.T) {
	scope := &fakeScope{clearAffected: 0}
	rebuilder, reader := newTestRebuilder(t, scope, 2)

	result, err := rebuilder.RebuildScope(context.Background(), "scope-a", PhaseDirtyRepair)
	if err != nil {
		t.Fatalf("RebuildScope() error = %v", err)
	}
	if result.Outcome != OutcomeLeftDirty {
		t.Fatalf("RebuildScope() outcome = %q, want %q", result.Outcome, OutcomeLeftDirty)
	}
	if got := collectMetrics(t, reader)["eshu_dp_package_manifest_backfill_scope_rebuilds_total|outcome=left_dirty,phase=dirty_repair"]; got != 1 {
		t.Fatalf("left_dirty repair rebuilds = %d, want 1", got)
	}
}

func TestRebuildScopeLockTimeoutRollsBackOnlyThatPage(t *testing.T) {
	lockTimeout := &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"}
	scope := &fakeScope{lockErrAt: 3, lockErr: lockTimeout, clearAffected: 1}
	rebuilder, reader := newTestRebuilder(t, scope, 7)

	result, err := rebuilder.RebuildScope(context.Background(), "scope-a", PhaseInitial)
	if !errors.Is(err, lockTimeout) {
		t.Fatalf("RebuildScope() error = %v, want the 55P03 error", err)
	}
	if result.Outcome != OutcomeLockTimeout || result.Pages != 1 || result.Facts != 3 {
		t.Fatalf("RebuildScope() = %+v, want lock_timeout after 1 committed page of 3 facts", result)
	}
	if scope.commits != 2 || scope.rollbacks != 1 {
		t.Fatalf("committed/rolled back = %d/%d, want start and page 1 committed, page 2 rolled back", scope.commits, scope.rollbacks)
	}
	metrics := collectMetrics(t, reader)
	if got := metrics["eshu_dp_package_manifest_backfill_page_duration_seconds|outcome=lock_timeout"]; got != 1 {
		t.Fatalf("lock_timeout transaction observations = %d, want 1 (metrics %v)", got, metrics)
	}
	if got := metrics["eshu_dp_package_manifest_backfill_scope_rebuilds_total|outcome=lock_timeout,phase=initial"]; got != 1 {
		t.Fatalf("lock_timeout rebuilds = %d, want 1", got)
	}
}

func TestRebuildScopeRequiresSeams(t *testing.T) {
	if _, err := (Rebuilder{}).RebuildScope(context.Background(), "scope-a", PhaseInitial); err == nil {
		t.Fatal("RebuildScope() with no seams = nil error, want a configuration error")
	}
}

// collectMetrics flattens histogram counts and counter sums into
// "name|sorted attrs" keys.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	out := make(map[string]uint64)
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					out[m.Name+"|"+point.Attributes.Encoded(attribute.DefaultEncoder())] += point.Count
				}
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					out[m.Name+"|"+point.Attributes.Encoded(attribute.DefaultEncoder())] += uint64(point.Value)
				}
			}
		}
	}
	return out
}
