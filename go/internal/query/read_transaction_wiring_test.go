// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// jitWiringReader is the serve adapter's reader: a guarded read store whose
// snapshot transaction records what runs inside it. Statements outside a
// transaction (the supports readiness probe) are answered ready.
type jitWiringReader struct {
	mu     sync.Mutex
	events []string
}

func (r *jitWiringReader) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *jitWiringReader) snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.events, ",")
}

type jitWiringRows struct {
	rows [][]any
	pos  int
}

func (r *jitWiringRows) Next() bool { r.pos++; return r.pos <= len(r.rows) }
func (r *jitWiringRows) Scan(dest ...any) error {
	for i := range dest {
		if flag, ok := dest[i].(*bool); ok {
			*flag = true
		}
	}
	return nil
}
func (r *jitWiringRows) Err() error   { return nil }
func (r *jitWiringRows) Close() error { return nil }

type jitWiringRow struct{}

func (jitWiringRow) Scan(dest ...any) error {
	for i := range dest {
		if text, ok := dest[i].(*string); ok {
			*text = "off"
		}
	}
	return nil
}

func (r *jitWiringReader) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
		return &jitWiringRows{rows: [][]any{{true}}}, nil
	}
	r.record("pool-read")
	return &jitWiringRows{}, nil
}

func (r *jitWiringReader) QueryRowContext(context.Context, string, ...any) db.Row {
	r.record("pool-row")
	return jitWiringRow{}
}

func (r *jitWiringReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	r.record("begin")
	return &jitWiringTx{reader: r}, nil
}

type jitWiringTx struct{ reader *jitWiringReader }

func (tx *jitWiringTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if strings.Contains(query, "set_config('jit'") {
		tx.reader.record("jit_off")
	} else {
		tx.reader.record("tx-read")
	}
	return &jitWiringRows{}, nil
}

func (tx *jitWiringTx) QueryRowContext(_ context.Context, query string, _ ...any) db.Row {
	if strings.Contains(query, "set_config('jit'") {
		tx.reader.record("jit_off")
	} else {
		tx.reader.record("tx-row")
	}
	return jitWiringRow{}
}

func (tx *jitWiringTx) Commit() error   { tx.reader.record("commit"); return nil }
func (tx *jitWiringTx) Rollback() error { tx.reader.record("rollback"); return nil }

// queryerOnly hides the snapshot beginner of a reader: the seeded violation, a
// reader that is not a transaction source.
type queryerOnly struct{ inner db.Queryer }

func (q queryerOnly) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return q.inner.QueryContext(ctx, query, args...)
}

// precisePageOpensAJITScope runs the unscoped precise list on store and reports
// whether the read ran inside a transaction that switched jit off first.
func precisePageOpensAJITScope(t *testing.T, reader *jitWiringReader, store impact.FindingStore) bool {
	t.Helper()
	reader.mu.Lock()
	reader.events = nil
	reader.mu.Unlock()
	if _, err := store.ListSupplyChainImpactFindings(t.Context(),
		impact.FindingFilter{Limit: 10, Status: "affected_exact", DetectionProfile: impact.ProfilePrecise}); err != nil {
		t.Fatalf("list = %v", err)
	}
	return strings.Contains(reader.snapshot(), "begin,jit_off,tx-read,commit")
}

// TestSupplyChainStoresReadThroughATransactionSource pins ruling decision 13: the
// production serve adapter hands the impact stores a reader that is a transaction
// source, so the jit-off scope of the supplement and image routes opens in
// production and the plain branch of the scope helper cannot run unnoticed. The
// seeded violation builds a store over a reader without the interface and shows the
// same probe reports no scope.
func TestSupplyChainStoresReadThroughATransactionSource(t *testing.T) {
	t.Parallel()
	reader := &jitWiringReader{}
	handler := newCoreSupplyChainHandler(nil, reader, nil, nil, ProfileProduction, true, nil, nil)
	if handler.ImpactFindings == nil {
		t.Fatal("the serve adapter wired no impact finding store")
	}
	if !precisePageOpensAJITScope(t, reader, handler.ImpactFindings) {
		t.Fatalf("the production wiring did not open a jit-off read transaction for the precise list; events: %s", reader.snapshot())
	}

	// Seeded violation: a reader that hides BeginReadOnlySnapshot.
	violating := impact.NewPostgresFindingStoreWithReadStore(queryerOnly{inner: reader}, true)
	if precisePageOpensAJITScope(t, reader, violating) {
		t.Fatal("the probe reported a jit-off scope for a reader that is not a transaction source: it would not catch a wiring regression")
	}
}
