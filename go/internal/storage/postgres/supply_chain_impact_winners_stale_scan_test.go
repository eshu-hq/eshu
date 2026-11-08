// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Fakes for the #10416 slice-1 winners-audit split scan. They exercise the
// production AuditWinners path end to end with a stubbed transport: every SQL
// statement the store issues is recorded, so the tests prove which statements
// run, in which order, and with which batch shapes, without a database.

// staleScanFakeRows replays canned candidate rows for the stale candidate
// source. A nil cell scans as SQL NULL, proving the seed preserves nulls the
// way the legacy temp-table copy does.
type staleScanFakeRows struct {
	rows [][]any
	pos  int
	err  error
}

func (r *staleScanFakeRows) Next() bool { return r.pos < len(r.rows) }

func (r *staleScanFakeRows) Scan(dest ...any) error {
	if r.pos >= len(r.rows) {
		return sql.ErrNoRows
	}
	row := r.rows[r.pos]
	r.pos++
	if len(dest) != len(row) {
		return errors.New("stale scan fake: destination count mismatch")
	}
	for i, d := range dest {
		nullable, ok := d.(*sql.NullString)
		if !ok {
			return errors.New("stale scan fake: destination must be *sql.NullString")
		}
		if row[i] == nil {
			*nullable = sql.NullString{}
			continue
		}
		s, ok := row[i].(string)
		if !ok {
			return errors.New("stale scan fake: cell must be a string or nil")
		}
		*nullable = sql.NullString{String: s, Valid: true}
	}
	return nil
}

func (r *staleScanFakeRows) Err() error { return r.err }

func (r *staleScanFakeRows) Close() error { return nil }

// staleScanFakeQueryer is the stubbed stale candidate source.
type staleScanFakeQueryer struct {
	rows [][]any
	err  error
	// queries records every statement the store sends to the stale source.
	queries []string
}

func (f *staleScanFakeQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}
	return &staleScanFakeRows{rows: f.rows}, nil
}

// staleScanFakeDeferral is a refused stale read that is NOT the production
// type: matching must work through the marker method alone.
type staleScanFakeDeferral struct{}

func (staleScanFakeDeferral) Error() string           { return "stale scan fake: deferred" }
func (staleScanFakeDeferral) StaleReadDeferred() bool { return true }

// staleScanFakeTx records every writer statement and answers the count, claim
// and queue statements with canned results.
type staleScanFakeTx struct {
	execs      []staleScanExec
	queries    []string
	count      int64
	claim      bool
	repairs    int64
	committed  bool
	rolledBack bool
}

type staleScanExec struct {
	query string
	args  []any
}

type staleScanFakeResult struct{ n int64 }

func (r staleScanFakeResult) LastInsertId() (int64, error) {
	return 0, errors.New("stale scan fake: no insert id")
}

func (r staleScanFakeResult) RowsAffected() (int64, error) { return r.n, nil }

func (tx *staleScanFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.execs = append(tx.execs, staleScanExec{query: query, args: args})
	return staleScanFakeResult{n: tx.repairs}, nil
}

func (tx *staleScanFakeTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	tx.queries = append(tx.queries, query)
	switch {
	case strings.Contains(query, "pg_try_advisory_xact_lock"):
		return &staleScanBoolRows{value: tx.claim}, nil
	case strings.Contains(query, "count(*)"):
		return &staleScanIntRows{values: []int64{tx.count}}, nil
	default:
		return &staleScanIntRows{}, nil
	}
}

func (tx *staleScanFakeTx) Commit() error {
	tx.committed = true
	return nil
}

func (tx *staleScanFakeTx) Rollback() error {
	tx.rolledBack = true
	return nil
}

type staleScanBoolRows struct {
	value bool
	done  bool
}

func (r *staleScanBoolRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *staleScanBoolRows) Scan(dest ...any) error {
	claimed, ok := dest[0].(*bool)
	if !ok {
		return errors.New("stale scan fake: claim destination must be *bool")
	}
	*claimed = r.value
	return nil
}

func (r *staleScanBoolRows) Err() error { return nil }

func (r *staleScanBoolRows) Close() error { return nil }

type staleScanIntRows struct {
	values []int64
	pos    int
}

func (r *staleScanIntRows) Next() bool { return r.pos < len(r.values) }

func (r *staleScanIntRows) Scan(dest ...any) error {
	if r.pos >= len(r.values) {
		return sql.ErrNoRows
	}
	n, ok := dest[0].(*int64)
	if !ok {
		return errors.New("stale scan fake: count destination must be *int64")
	}
	*n = r.values[r.pos]
	r.pos++
	return nil
}

func (r *staleScanIntRows) Err() error { return nil }

func (r *staleScanIntRows) Close() error { return nil }

// staleScanFakeBeginner counts writer-transaction opens: the split scan must
// not begin one when there is nothing to verify or when the stale read
// refuses.
type staleScanFakeBeginner struct {
	begins int
	tx     *staleScanFakeTx
	err    error
}

func (f *staleScanFakeBeginner) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("stale scan fake: unexpected QueryContext on the writer handle")
}

func (f *staleScanFakeBeginner) Begin(context.Context) (db.Transaction, error) {
	f.begins++
	if f.err != nil {
		return nil, f.err
	}
	return f.tx, nil
}

// TestWinnersAuditLegacyStatementsUnchanged pins the legacy writer path's
// statement set and order. Slice 1 adds a split-scan branch beside it; this
// test fails if that branch disturbs a single legacy statement. The
// expectations derive from the shipped constants (never a hand copy), so they
// prove the refactor preserved the set and order rather than freezing a copy.
func TestWinnersAuditLegacyStatementsUnchanged(t *testing.T) {
	t.Parallel()

	tx := &staleScanFakeTx{count: 1, claim: true, repairs: 2}
	beginner := &staleScanFakeBeginner{tx: tx}
	store := SupplyChainImpactWinnersStore{DB: beginner}

	audit, err := store.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners() error = %v", err)
	}
	if audit.Repairs != 2 || audit.Skipped || audit.Deferred {
		t.Fatalf("AuditWinners() = %+v, want 2 repairs, neither skipped nor deferred", audit)
	}

	wantExecs := []string{
		createWinnersAuditCandidatesSQL,
		seedWinnersAuditIdentsSQL,
		analyzeWinnersKeyedIdentsSQL,
		buildWinnersKeyedActiveFactsSQL,
		addWinnersKeyedOperatorFactsSQL,
		analyzeWinnersKeyedActiveFactsSQL,
		createWinnersAuditStaleSQL,
		createWinnersAuditPendingSQL,
		queueWinnersAuditRepairsSQL,
	}
	if len(tx.execs) != len(wantExecs) {
		t.Fatalf("legacy exec count = %d, want %d: %v", len(tx.execs), len(wantExecs), execQueries(tx.execs))
	}
	for i, want := range wantExecs {
		if tx.execs[i].query != want {
			t.Errorf("legacy exec %d differs:\n got: %q\nwant: %q", i, tx.execs[i].query, want)
		}
	}
	if beginner.begins != 1 || !tx.committed {
		t.Errorf("legacy path begins = %d committed = %v, want exactly one committed transaction",
			beginner.begins, tx.committed)
	}
}

func execQueries(execs []staleScanExec) []string {
	out := make([]string, 0, len(execs))
	for _, e := range execs {
		out = append(out, e.query)
	}
	return out
}

// TestWinnersAuditSplitScanIsAPlainSelect proves the stale candidate statement
// is a pure single-statement SELECT: no DDL may ride the read store, whose
// surface cannot Exec.
func TestWinnersAuditSplitScanIsAPlainSelect(t *testing.T) {
	t.Parallel()

	upper := strings.ToUpper(selectWinnersAuditCandidatesSQL)
	for _, forbidden := range []string{"CREATE", "INSERT", "UPDATE", "DELETE", "DROP", "ALTER", ";"} {
		if strings.Contains(upper, forbidden) {
			t.Errorf("candidate SELECT contains %q: %q", forbidden, selectWinnersAuditCandidatesSQL)
		}
	}
	if !strings.Contains(upper, "SELECT") {
		t.Errorf("candidate SELECT has no SELECT: %q", selectWinnersAuditCandidatesSQL)
	}
	if !strings.Contains(selectWinnersAuditCandidatesSQL, "supply_chain_impact_canonical_winners") {
		t.Errorf("candidate SELECT never diffs the stored winners: %q", selectWinnersAuditCandidatesSQL)
	}
}

// TestWinnersAuditSplitZeroCandidatesOpensNoWriterTx proves an empty stale
// read ends the tick before the writer transaction begins.
func TestWinnersAuditSplitZeroCandidatesOpensNoWriterTx(t *testing.T) {
	t.Parallel()

	stale := &staleScanFakeQueryer{}
	beginner := &staleScanFakeBeginner{tx: &staleScanFakeTx{claim: true}}
	store := SupplyChainImpactWinnersStore{DB: beginner}.WithStaleCandidates(stale)

	audit, err := store.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners() error = %v", err)
	}
	if audit.Repairs != 0 || audit.Skipped || audit.Deferred {
		t.Fatalf("AuditWinners() = %+v, want a clean pass", audit)
	}
	if len(stale.queries) != 1 || stale.queries[0] != selectWinnersAuditCandidatesSQL {
		t.Fatalf("stale statements = %v, want exactly the candidate SELECT", stale.queries)
	}
	if beginner.begins != 0 {
		t.Fatalf("writer begins = %d, want none: zero candidates must not open a writer tx", beginner.begins)
	}
}

// TestWinnersAuditSplitDeferralEndsTheTick proves a refused stale read ends
// the tick as a typed deferred outcome: no writer tx, no error, no fallback
// repair.
func TestWinnersAuditSplitDeferralEndsTheTick(t *testing.T) {
	t.Parallel()

	stale := &staleScanFakeQueryer{err: staleScanFakeDeferral{}}
	beginner := &staleScanFakeBeginner{tx: &staleScanFakeTx{claim: true}}
	store := SupplyChainImpactWinnersStore{DB: beginner}.WithStaleCandidates(stale)

	audit, err := store.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners() error = %v, want nil: a deferral is an outcome, not a failure", err)
	}
	if !audit.Deferred || audit.Skipped || audit.Repairs != 0 {
		t.Fatalf("AuditWinners() = %+v, want a deferred outcome only", audit)
	}
	if beginner.begins != 0 {
		t.Fatalf("writer begins = %d, want none: a deferral must not touch the writer", beginner.begins)
	}
}

// TestWinnersAuditSplitStaleErrorIsAnError proves a non-deferral stale failure
// stays an error (and still opens no writer tx): only a refused read defers.
func TestWinnersAuditSplitStaleErrorIsAnError(t *testing.T) {
	t.Parallel()

	boom := errors.New("stale scan fake: connection refused")
	stale := &staleScanFakeQueryer{err: boom}
	beginner := &staleScanFakeBeginner{tx: &staleScanFakeTx{claim: true}}
	store := SupplyChainImpactWinnersStore{DB: beginner}.WithStaleCandidates(stale)

	audit, err := store.AuditWinners(context.Background())
	if err == nil {
		t.Fatalf("AuditWinners() = %+v, want the stale failure as an error", audit)
	}
	if audit.Deferred {
		t.Fatalf("AuditWinners() = %+v, a transport failure must not report deferred", audit)
	}
	if beginner.begins != 0 {
		t.Fatalf("writer begins = %d, want none", beginner.begins)
	}
}

// TestWinnersAuditSplitSeedsInBoundedBatches proves the ident seed crosses the
// batch boundary in bounded INSERTs: memory is bounded by the batch, never by
// the candidate count, and every candidate is seeded exactly once, in order.
func TestWinnersAuditSplitSeedsInBoundedBatches(t *testing.T) {
	t.Parallel()

	const candidates = 2*winnersAuditStaleSeedBatchSize + 1
	rows := make([][]any, 0, candidates)
	for i := 0; i < candidates; i++ {
		rows = append(rows, []any{
			"pkg:" + staleScanItoa(i), "repo", "digest", "CVE-2026-0001", "ADV-1",
		})
	}
	// One NULL advisory proves the seed preserves nulls like the legacy
	// temp-table copy instead of coercing them to empty.
	rows[0][4] = nil

	stale := &staleScanFakeQueryer{rows: rows}
	tx := &staleScanFakeTx{claim: true, repairs: int64(candidates)}
	beginner := &staleScanFakeBeginner{tx: tx}
	store := SupplyChainImpactWinnersStore{DB: beginner}.WithStaleCandidates(stale)

	audit, err := store.AuditWinners(context.Background())
	if err != nil {
		t.Fatalf("AuditWinners() error = %v", err)
	}
	if audit.Repairs != int64(candidates) {
		t.Fatalf("AuditWinners() repairs = %d, want %d", audit.Repairs, candidates)
	}

	var seeds []staleScanExec
	for _, e := range tx.execs {
		if e.query == seedWinnersAuditStaleIdentsSQL {
			seeds = append(seeds, e)
		}
	}
	if len(seeds) != 3 {
		t.Fatalf("seed batches = %d, want 3 for %d candidates", len(seeds), candidates)
	}
	var total int
	for b, seed := range seeds {
		if len(seed.args) != 5 {
			t.Fatalf("seed batch %d has %d arg arrays, want 5", b, len(seed.args))
		}
		n := -1
		for a, arg := range seed.args {
			arr, ok := arg.([]any)
			if !ok {
				t.Fatalf("seed batch %d arg %d is %T, want []any", b, a, arg)
			}
			if n < 0 {
				n = len(arr)
			} else if len(arr) != n {
				t.Fatalf("seed batch %d arg %d length = %d, want %d", b, a, len(arr), n)
			}
		}
		if b < 2 && n != winnersAuditStaleSeedBatchSize {
			t.Fatalf("seed batch %d rows = %d, want full batch %d", b, n, winnersAuditStaleSeedBatchSize)
		}
		if b == 2 && n != 1 {
			t.Fatalf("seed batch %d rows = %d, want the 1 remainder", b, n)
		}
		total += n
	}
	if total != candidates {
		t.Fatalf("seeded idents = %d, want every one of the %d candidates", total, candidates)
	}
	// Batch 0 carries the NULL advisory: arg index 4 is the advisory_id array.
	if seeds[0].args[4].([]any)[0] != nil {
		t.Errorf("NULL advisory_id was not preserved through the seed")
	}
	if got := seeds[0].args[0].([]any)[0]; got != "pkg:0" {
		t.Errorf("first seeded package_id = %v, want pkg:0: order must be preserved", got)
	}
}

func staleScanItoa(i int) string { return strconv.Itoa(i) }
