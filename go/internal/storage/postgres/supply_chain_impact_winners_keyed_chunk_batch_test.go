// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// chunkBatchFakeRows is a canned db.Rows over an identifier list.
type chunkBatchFakeRows struct {
	idents [][5]string
	pos    int
}

func (r *chunkBatchFakeRows) Next() bool { return r.pos < len(r.idents) }

func (r *chunkBatchFakeRows) Scan(dest ...any) error {
	if r.pos >= len(r.idents) {
		return fmt.Errorf("scan past end of chunk batch fake rows")
	}
	if len(dest) != 5 {
		return fmt.Errorf("chunk batch fake rows scan wants 5 destinations, got %d", len(dest))
	}
	for i := 0; i < 5; i++ {
		ptr, ok := dest[i].(*string)
		if !ok {
			return fmt.Errorf("chunk batch fake rows scan destination %d is not *string", i)
		}
		*ptr = r.idents[r.pos][i]
	}
	r.pos++
	return nil
}

func (r *chunkBatchFakeRows) Err() error { return nil }

func (r *chunkBatchFakeRows) Close() error { return nil }

// chunkBatchFakeTx records every Exec and serves one canned identifier list
// for the chunk's SELECT. It proves the tick's statement shape without a
// database: the capacity lane (#10839) bounds on per-tick round trips.
type chunkBatchFakeTx struct {
	idents [][5]string
	execs  []string
	args   [][]any
	// probeScanned scripts RowsAffected for successive narrow-probe INSERTs,
	// oldest-first: the budgeted admission loop meters the scanned-copies
	// budget from these counts. Empty (the default) reads 0, so admission
	// never trips the budget on these fakes.
	probeScanned []int64
	probes       int
}

func (f *chunkBatchFakeTx) QueryContext(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return &chunkBatchFakeRows{idents: f.idents}, nil
}

func (f *chunkBatchFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, query)
	f.args = append(f.args, args)
	if isChunkNarrowProbeInsert(query) {
		n := int64(0)
		if f.probes < len(f.probeScanned) {
			n = f.probeScanned[f.probes]
		}
		f.probes++
		return fakeChunkBatchRowsAffected{n: n}, nil
	}
	return fakeChunkBatchResult{}, nil
}

// isChunkNarrowProbeInsert reports whether query appends one identifier's
// copies to the narrow table (as opposed to writing the chunk or staging
// tables): only those INSERTs carry a scanned-copy count.
func isChunkNarrowProbeInsert(query string) bool {
	trimmed := strings.TrimSpace(query)
	return strings.HasPrefix(strings.ToUpper(trimmed), "INSERT") && strings.Contains(query, winnersKeyedNarrowTable)
}

func (f *chunkBatchFakeTx) Commit() error   { return nil }
func (f *chunkBatchFakeTx) Rollback() error { return nil }

type fakeChunkBatchResult struct{}

func (fakeChunkBatchResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeChunkBatchResult) RowsAffected() (int64, error) { return 0, nil }

// fakeChunkBatchRowsAffected is the sql.Result of a scripted narrow-probe
// INSERT: RowsAffected is that identifier's canned scanned-copy count.
type fakeChunkBatchRowsAffected struct{ n int64 }

func (r fakeChunkBatchRowsAffected) LastInsertId() (int64, error) { return 0, nil }
func (r fakeChunkBatchRowsAffected) RowsAffected() (int64, error) { return r.n, nil }

// chunkBatchIdents builds n distinct canned identifiers.
func chunkBatchIdents(n int) [][5]string {
	out := make([][5]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, [5]string{
			fmt.Sprintf("pkg-%d", i),
			fmt.Sprintf("repo-%d", i),
			fmt.Sprintf("digest-%d", i),
			fmt.Sprintf("c0-%d", i),
			fmt.Sprintf("adv-%d", i),
		})
	}
	return out
}

// insertExecs counts the Exec calls that insert chunk identifiers.
func insertExecs(f *chunkBatchFakeTx) int {
	n := 0
	for _, q := range f.execs {
		trimmed := strings.TrimSpace(q)
		if strings.HasPrefix(strings.ToUpper(trimmed), "INSERT") && strings.Contains(q, "winners_keyed_chunk_idents") {
			n++
		}
	}
	return n
}

// TestBuildWinnersKeyedChunkBatchesIdentifierInserts is the #10839 theory
// shim in test form: a saturated 200-identifier tick must pack the whole
// chunk into one INSERT statement, not one round trip per identifier. The
// per-tick round-trip count is the capacity term this lane removes; the
// chunk bound, order, and truncate rule are unchanged.
func TestBuildWinnersKeyedChunkBatchesIdentifierInserts(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(201)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200, scannedCopies: 10000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 200 {
		t.Fatalf("identifiers = %d, want 200", outcome.identifiers)
	}
	if !outcome.truncated {
		t.Fatalf("truncated = false, want true for a 201-row identifier set against a 200 bound")
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1 batched statement", got)
	}
	// The chunk INSERT carries every admitted identifier in oldest-first
	// order. The budgeted admission stages identifiers one at a time beside
	// it (one staging row per probe), so only the chunk-table INSERT's args
	// are read here.
	var gotIDs [][5]string
	for k, q := range fake.execs {
		trimmed := strings.TrimSpace(q)
		if !strings.HasPrefix(strings.ToUpper(trimmed), "INSERT") || !strings.Contains(q, "winners_keyed_chunk_idents") {
			continue
		}
		argSet := fake.args[k]
		for i := 0; i+5 <= len(argSet); i += 5 {
			var id [5]string
			for j := 0; j < 5; j++ {
				s, ok := argSet[i+j].(string)
				if !ok {
					t.Fatalf("batched arg %d is %T, want string", i+j, argSet[i+j])
				}
				id[j] = s
			}
			gotIDs = append(gotIDs, id)
		}
	}
	if len(gotIDs) != 200 {
		t.Fatalf("batched identifiers = %d, want 200", len(gotIDs))
	}
	for i := range gotIDs {
		if gotIDs[i] != fake.idents[i] {
			t.Fatalf("batched identifier %d = %v, want %v (oldest-first order)", i, gotIDs[i], fake.idents[i])
		}
	}
}

// TestBuildWinnersKeyedChunkSingleIdentifierCompletes keeps the hot-partition
// rule: one identifier still drains in one INSERT, never zero.
func TestBuildWinnersKeyedChunkSingleIdentifierCompletes(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(1)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 1, scannedCopies: 10000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 1 || outcome.truncated {
		t.Fatalf("outcome = %+v, want {identifiers:1 truncated:false}", outcome)
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1", got)
	}
}

// TestBuildWinnersKeyedChunkEmptyInsertsNothing keeps the empty-window rule:
// no identifier INSERT runs when the set is empty.
func TestBuildWinnersKeyedChunkEmptyInsertsNothing(t *testing.T) {
	fake := &chunkBatchFakeTx{}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200, scannedCopies: 10000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 0 || outcome.truncated {
		t.Fatalf("outcome = %+v, want {identifiers:0 truncated:false}", outcome)
	}
	if got := insertExecs(fake); got != 0 {
		t.Fatalf("identifier INSERT statements = %d, want 0", got)
	}
}

// TestBuildWinnersKeyedChunkBatchClampAboveParameterCeiling is the #11157
// failing-first test: a chunk above the Postgres parameter ceiling (65,535
// params at 5 per row = 13,107 rows) must fail closed with an explicit bound
// error instead of sending an over-limit INSERT to the server.
func TestBuildWinnersKeyedChunkBatchClampAboveParameterCeiling(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(13109)}
	_, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 50000, identifiers: 13108, scannedCopies: 10000})
	if err == nil {
		t.Fatalf("buildWinnersKeyedChunk with 13108 identifiers: got nil error, want parameter-ceiling clamp")
	}
	if got := insertExecs(fake); got != 0 {
		t.Fatalf("identifier INSERT statements = %d, want 0 (clamp before Exec)", got)
	}
}

// TestWinnersChunkDefaults pins the keyed winners chunk bounds (eshu-hq/eshu#10839,
// Dec 6(ii)). The identifier bound stays 200: a 1000 bound was tried on QA and
// rolled back (the first tick took 710.8 s for 237,063 kept rows, 1.8x the
// per-kept-row cost of the 200 bound). The scanned-copies budget defaults to
// 10,000 pre-filter copies (Dec 5(a) unit cost over the 15 s bar). The
// zero-value store must resolve all three defaults, the default batch stays
// one INSERT far under the Postgres parameter ceiling (200 x 5 = 1000 params
// vs the 13107-row clamp), and a saturated default-bound tick admits exactly
// the bound in one statement with truncation (oldest-first order is pinned
// element-wise by TestBuildWinnersKeyedChunkBatchesIdentifierInserts).
func TestWinnersChunkDefaults(t *testing.T) {
	var empty SupplyChainImpactWinnersStore
	if got := empty.chunkBounds(); got.identifiers != 200 || got.dirtyRows != 5000 || got.scannedCopies != 10000 {
		t.Fatalf("default chunkBounds() = %+v, want {dirtyRows:5000 identifiers:200 scannedCopies:10000}", got)
	}
	if params := empty.chunkBounds().identifiers * 5; params > 65535 || empty.chunkBounds().identifiers > int64(maxWinnersKeyedChunkBatchIdents) {
		t.Fatalf("default batch is 1000 params at 200 identifiers, want under the 65535 parameter ceiling and the %d-row clamp", maxWinnersKeyedChunkBatchIdents)
	}
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(201)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, empty.chunkBounds())
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk at the default bound: %v", err)
	}
	if outcome.identifiers != 200 || !outcome.truncated {
		t.Fatalf("outcome = %+v, want {identifiers:200 truncated:true}", outcome)
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1 batched statement", got)
	}
}

// TestBuildWinnersKeyedChunkBatchExactCeilingSucceeds pins the clamp boundary:
// exactly maxWinnersKeyedChunkBatchIdents (13,107 = 65,535/5) identifiers must
// still build one INSERT (the clamp fires strictly above the ceiling).
func TestBuildWinnersKeyedChunkBatchExactCeilingSucceeds(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(13107)}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 50000, identifiers: 13107, scannedCopies: 10000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk with 13107 identifiers: %v, want success at the exact ceiling", err)
	}
	if outcome.identifiers != 13107 {
		t.Fatalf("identifiers = %d, want 13107", outcome.identifiers)
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1", got)
	}
}

// TestBuildWinnersKeyedChunkScannedBudgetTruncates is the Dec 6(ii) unit
// proof: with 5 identifiers at 300 scanned copies each and a 1000-copy
// budget, admission stops before the fifth (300, 600, 900 admitted, 1200
// trips the check), the chunk carries exactly the admitted oldest-first
// prefix in one INSERT, and the outcome names the budget.
func TestBuildWinnersKeyedChunkScannedBudgetTruncates(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(5), probeScanned: []int64{300, 300, 300, 300, 300}}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200, scannedCopies: 1000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 4 {
		t.Fatalf("identifiers = %d, want 4 (budget 1000 at 300 scanned each)", outcome.identifiers)
	}
	if outcome.truncated || !outcome.budgetTruncated {
		t.Fatalf("outcome = %+v, want {truncated:false budgetTruncated:true} (budget-only trip)", outcome)
	}
	if got := insertExecs(fake); got != 1 {
		t.Fatalf("identifier INSERT statements = %d, want 1 batched statement", got)
	}
	if got := winnersChunkTruncatedBy(outcome); got != supplychaincore.WinnersChunkTruncatedByScannedCopies {
		t.Fatalf("truncated by = %q, want %q", got, supplychaincore.WinnersChunkTruncatedByScannedCopies)
	}
}

// TestBuildWinnersKeyedChunkFirstIdentifierCompletesOverBudget pins the
// hot-identifier rule under the budget: a single identifier scanning past
// the budget still completes (never split, never stalled), and the tick
// reports truncation while identifiers stay queued.
func TestBuildWinnersKeyedChunkFirstIdentifierCompletesOverBudget(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(2), probeScanned: []int64{240, 240}}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200, scannedCopies: 100})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 1 {
		t.Fatalf("identifiers = %d, want exactly the first (it always completes)", outcome.identifiers)
	}
	if outcome.truncated || !outcome.budgetTruncated {
		t.Fatalf("outcome = %+v, want {truncated:false budgetTruncated:true} (budget-only trip)", outcome)
	}
}

// TestBuildWinnersKeyedChunkBothBoundsTruncate pins the both-bounds outcome:
// an identifier set past the identifier bound whose budgeted prefix then
// spends the scanned-copies budget reports both.
func TestBuildWinnersKeyedChunkBothBoundsTruncate(t *testing.T) {
	fake := &chunkBatchFakeTx{idents: chunkBatchIdents(205), probeScanned: []int64{300, 300, 300, 300}}
	outcome, err := buildWinnersKeyedChunk(context.Background(), fake, winnersChunkBounds{dirtyRows: 5000, identifiers: 200, scannedCopies: 1000})
	if err != nil {
		t.Fatalf("buildWinnersKeyedChunk: %v", err)
	}
	if outcome.identifiers != 4 {
		t.Fatalf("identifiers = %d, want 4 (budget trips inside the 200-identifier prefix)", outcome.identifiers)
	}
	if !outcome.truncated || !outcome.budgetTruncated {
		t.Fatalf("outcome = %+v, want {truncated:true budgetTruncated:true}", outcome)
	}
	if got := winnersChunkTruncatedBy(outcome); got != supplychaincore.WinnersChunkTruncatedByBoth {
		t.Fatalf("truncated by = %q, want %q", got, supplychaincore.WinnersChunkTruncatedByBoth)
	}
}

// TestWinnersChunkTruncatedByReason pins the truncation-reason mapping: the
// log distinguishes a budget trip from an identifier-bound trip, and a
// drained tick reports neither. truncated keeps its identifier-bound meaning
// (the builder sets it only for the identifier bound), so the pair reads
// unambiguously.
func TestWinnersChunkTruncatedByReason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		outcome winnersChunkOutcome
		want    string
	}{
		{"drained", winnersChunkOutcome{identifiers: 3}, ""},
		{"identifiers", winnersChunkOutcome{identifiers: 200, truncated: true}, supplychaincore.WinnersChunkTruncatedByIdentifiers},
		{"scanned copies", winnersChunkOutcome{identifiers: 4, budgetTruncated: true}, supplychaincore.WinnersChunkTruncatedByScannedCopies},
		{"both", winnersChunkOutcome{identifiers: 4, truncated: true, budgetTruncated: true}, supplychaincore.WinnersChunkTruncatedByBoth},
	} {
		if got := winnersChunkTruncatedBy(tc.outcome); got != tc.want {
			t.Errorf("%s: truncated by = %q, want %q", tc.name, got, tc.want)
		}
	}
}
