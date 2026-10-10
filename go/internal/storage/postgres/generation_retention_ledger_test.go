// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"
)

// retentionTestScopes returns n copies of scopeID, the scope ids countRows
// takes in parallel with a single-scope test's generation ids.
func retentionTestScopes(scopeID string, n int) []string {
	scopes := make([]string, n)
	for i := range scopes {
		scopes[i] = scopeID
	}
	return scopes
}

// ledgerRetentionFake is one prunable candidate with 5 fact rows and 3
// changed-since ledger rows (#7127 ruling 2.8).
func ledgerRetentionFake(now time.Time) *generationRetentionFakeDB {
	return &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-ledger", "generation-ledger", "repository",
			now.Add(-10 * 24 * time.Hour), now.Add(-11 * 24 * time.Hour),
			false,
		}},
		countRows: [][]any{{"generation-ledger", "fact_records", int64(5)}},
		ledgerCountRows: [][]any{
			{"generation-ledger", "changed_since_links", int64(1)},
			{"generation-ledger", "changed_since_link_deltas", int64(1)},
			{"generation-ledger", "changed_since_activations", int64(1)},
		},
		ledgerDeleted: []any{int64(1), int64(1), int64(0), int64(1)},
	}
}

func ledgerRetentionPolicy(rowLimit int) GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            rowLimit,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	}
}

// TestGenerationRetentionRowLimitCountsChangedSinceLedgerRows proves the
// store adds the ledger's row counts to the batch total BatchRowLimit is
// checked against. generation-ledger (5 fact rows + 3 ledger rows) fills a
// limit of 8, so the newer generation-small (1 fact row) waits for the next
// batch; counting facts alone (5 + 1) would have admitted both.
func TestGenerationRetentionRowLimitCountsChangedSinceLedgerRows(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := ledgerRetentionFake(now)
	database.candidateRows = append(database.candidateRows, []any{
		"scope-ledger", "generation-small", "repository", now.Add(-9 * 24 * time.Hour), now.Add(-10 * 24 * time.Hour),
		false,
	})
	database.countRows = append(database.countRows, []any{"generation-small", "fact_records", int64(1)})
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(8))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 1 || result.Skipped["row_limit"] != 1 || result.RowsOverLimit != 0 {
		t.Fatalf("pruned %d, skipped %v, over by %d; want generation-ledger alone and generation-small deferred",
			result.GenerationsPruned, result.Skipped, result.RowsOverLimit)
	}
}

// TestGenerationRetentionRowLimitSkipOverFactsAloneReportsOwnRowsReason proves
// a candidate whose facts alone exceed the limit reports row_limit_own_rows
// (#7334 fix 3 renamed this from row_limit): its own rows already exceed the
// limit, so no recount can ever admit it, unlike a row_limit_ledger skip.
func TestGenerationRetentionRowLimitSkipOverFactsAloneReportsOwnRowsReason(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := ledgerRetentionFake(now)
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(4))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.Skipped["row_limit_own_rows"] != 1 || result.Skipped["row_limit_ledger"] != 0 || result.Skipped["row_limit"] != 0 {
		t.Fatalf("skipped %v; want one row_limit_own_rows skip", result.Skipped)
	}
}

// TestGenerationRetentionPrunesChangedSinceLedgerBeforeGenerations proves a
// batch deletes the ledger rows, reports them in RowsPruned by ledger table,
// and does so before the scope_generations delete the ledger statement
// depends on to find the scopes.
func TestGenerationRetentionPrunesChangedSinceLedgerBeforeGenerations(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := ledgerRetentionFake(now)
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(8))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	for table, want := range map[string]int64{
		"changed_since_links":              1,
		"changed_since_link_deltas":        1,
		"changed_since_link_bucket_counts": 0,
		"changed_since_activations":        1,
	} {
		got, ok := result.RowsPruned[table]
		if !ok || got != want {
			t.Errorf("RowsPruned[%s] = %d (present %v), want %d", table, got, ok, want)
		}
	}
	// The pre-count (3 rows: 1 link, 1 delta, 1 activation) and the delete (the
	// fake returns the same) are reported apart, so the runner can compare them.
	wantLedger := map[string]int64{
		"changed_since_links": 1, "changed_since_link_deltas": 1,
		"changed_since_link_bucket_counts": 0, "changed_since_activations": 1,
	}
	if !maps.Equal(result.LedgerRowsPruned, wantLedger) {
		t.Errorf("LedgerRowsPruned = %v, want %v", result.LedgerRowsPruned, wantLedger)
	}
	if !maps.Equal(result.LedgerRowsCounted, wantLedger) {
		t.Errorf("LedgerRowsCounted = %v, want %v", result.LedgerRowsCounted, wantLedger)
	}
	ledgerAt, generationsAt := -1, -1
	for i, statement := range database.statements {
		switch {
		case strings.Contains(statement, "del_activations"):
			ledgerAt = i
		case strings.Contains(statement, "DELETE FROM scope_generations"):
			generationsAt = i
		}
	}
	if ledgerAt < 0 || generationsAt < 0 || ledgerAt > generationsAt {
		t.Fatalf("ledger delete at %d, scope_generations delete at %d; want the ledger first", ledgerAt, generationsAt)
	}
}

// TestRowLimitSkipReason pins the three cases: over only because of the
// ledger, over on other rows alone, and fitting alone in a full batch.
func TestRowLimitSkipReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows map[string]int64
		want string
	}{
		{"ledger pushes over", map[string]int64{"fact_records": 5, "changed_since_link_deltas": 3}, "row_limit_ledger"},
		{"facts alone over", map[string]int64{"fact_records": 9, "changed_since_link_deltas": 3}, "row_limit_own_rows"},
		{"fits alone, batch full", map[string]int64{"fact_records": 5, "changed_since_link_deltas": 1}, "row_limit"},
	} {
		if got := rowLimitSkipReason(tc.rows, 7); got != tc.want {
			t.Errorf("%s: rowLimitSkipReason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestGenerationRetentionRechecksLimitWhenRecountGrows proves the row limit
// holds after a recount that grows. The newer candidate is skipped, so the
// link between the two is no longer charged to it; the older candidate's
// prune still deletes that link (its prior side), so the recount charges it
// to the older one. That candidate is re-checked against the limit: over it
// only because of ledger rows, it is pruned alone.
func TestGenerationRetentionRechecksLimitWhenRecountGrows(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-l", "generation-older", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
			{"scope-l", "generation-newer", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
		},
		countRows: [][]any{
			{"generation-older", "fact_records", int64(5)},
			{"generation-newer", "fact_records", int64(50)},
		},
		// First count: the (older -> newer) link and its 40 deltas are the
		// newer's. The newer (50 + 42 rows) is over 60; the older (5 + 1) fits.
		ledgerCountRows: [][]any{
			{"generation-older", "changed_since_links", int64(1)},
			{"generation-newer", "changed_since_links", int64(2)},
			{"generation-newer", "changed_since_link_deltas", int64(40)},
		},
		// Recount of the older alone: the link it shares with the retained
		// newer is still deleted with it, 5 + 2 + 60 = 67 > 60.
		ledgerRecountRows: [][]any{
			{"generation-older", "changed_since_links", int64(2)},
			{"generation-older", "changed_since_link_deltas", int64(60)},
		},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(60))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	// The older generation's recount (5 + 2 + 60 = 67) is over 60 only
	// because of its ledger rows, so it is pruned in a batch of its own
	// (arbiter ruling arb-7127-3d-b), over the limit by 7; the newer waits.
	if result.GenerationsPruned != 1 || result.RowsOverLimit != 7 {
		t.Fatalf("pruned %d over by %d (skipped %v); want the older alone, over by 7", result.GenerationsPruned, result.RowsOverLimit, result.Skipped)
	}
	if got := generationRetentionRowsTotal(result.LedgerRowsCounted); got != 62 {
		t.Fatalf("batch counted %d ledger rows, want 62 (2 links, 60 deltas; with 5 facts, 67)", got)
	}
	if result.Skipped["row_limit_ledger"] != 1 {
		t.Fatalf("skipped %v, want the newer deferred as row_limit_ledger", result.Skipped)
	}
}

// TestGenerationRetentionDeletesTheLedgerInOneStatement is P2 (a) of arbiter
// ruling arb-7127-3d: of every statement one prune issues, exactly one deletes
// from the changed-since ledger. Split deletes leak a late link's deltas
// (TestSplitLedgerDeleteLeaksHeadlessDeltas).
func TestGenerationRetentionDeletesTheLedgerInOneStatement(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	database := ledgerRetentionFake(now)
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	if _, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(8)); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	var ledgerDeletes int
	for _, statement := range database.statements {
		if strings.Contains(statement, "DELETE FROM changed_since_") {
			ledgerDeletes++
			for _, table := range []string{
				"changed_since_links", "changed_since_link_deltas",
				"changed_since_link_bucket_counts", "changed_since_activations",
			} {
				if !strings.Contains(statement, "DELETE FROM "+table+" ") {
					t.Errorf("the ledger delete statement does not delete from %s", table)
				}
			}
		}
	}
	if ledgerDeletes != 1 {
		t.Fatalf("%d statements delete from the ledger, want exactly 1", ledgerDeletes)
	}
}
