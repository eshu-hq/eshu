// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that every relationship retention delete guards itself and the batch is bounded: each of the evidence, candidate and generation row deletes, called directly with an array holding every E1 to E8 generation (the ids a wrong caller could pass), removes only the rows of generations that satisfy Rule A and leaves the retained, stuck-active, pointer-named, pending and refinalize generations whole; the generation row delete removes nothing while a generation still has evidence rows or candidate rows, each kind checked on its own; on generation rows with no children, so no child guard can mask it, the generation row delete leaves an active row, a row a scope_generations row names and a row a scope pointer names and removes only the plain orphan; the settle read returns a claimed generation whose only remaining row is its generation row; and a pass with a small batch size and a row cap stops after the batch that crosses the cap and leaves the rest for the next pass
//eshu:readiness:env ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"database/sql"
	"fmt"
	"testing"

	relationshipretention "github.com/eshu-hq/eshu/go/internal/storage/postgres/retention/relationships"
)

// TestDeleteStatementsGuardThemselvesLive calls each delete statement directly
// with an array of every matrix generation, the way a caller that trusted the
// wrong ids would. Each statement must remove only what Rule A allows, in its
// own snapshot, with no help from the classification statement: that is what
// makes the deletes self-guarding rather than correct only when fed correctly.
// The generation row delete also requires that no evidence and no candidate row
// of the generation remain, so it runs after the two deletes that empty them.
func TestDeleteStatementsGuardThemselvesLive(t *testing.T) {
	f := openLiveFixture(t)
	ids := f.eligibilityMatrix(t)
	all := make([]string, 0, len(ids))
	for _, id := range ids {
		all = append(all, id)
	}

	retained := []string{"e1", "e3", "e4", "e6", "e7"} // must never lose a row to any delete
	for _, tc := range []struct {
		name      string
		before    []string // statements the same transaction ran first, empty their children
		statement string
		want      int64 // rows removed from the swept generations e2, e5, e8
		table     string
	}{
		{"evidence", nil, relationshipretention.DeleteEvidenceSQL, 9, "relationship_evidence_facts"},
		{"candidates", nil, relationshipretention.DeleteCandidatesSQL, 6, "relationship_candidates"},
		{
			"generations after the children are gone",
			[]string{relationshipretention.DeleteEvidenceSQL, relationshipretention.DeleteCandidatesSQL},
			relationshipretention.DeleteGenerationsSQL, 2, "relationship_generations",
		},
		{"generations while the children remain", nil, relationshipretention.DeleteGenerationsSQL, 0, "relationship_generations"},
		// Each child guard on its own: with only one kind of child left, the other
		// guard has nothing to block on, so a statement that lost this one would
		// delete the generation row while rows of it remain.
		{
			"generations while only evidence remains",
			[]string{relationshipretention.DeleteCandidatesSQL},
			relationshipretention.DeleteGenerationsSQL, 0, "relationship_generations",
		},
		{
			"generations while only candidates remain",
			[]string{relationshipretention.DeleteEvidenceSQL},
			relationshipretention.DeleteGenerationsSQL, 0, "relationship_generations",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := f.db.BeginTx(f.ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			for _, statement := range tc.before {
				if _, err := tx.ExecContext(f.ctx, statement, deleteArgs(statement, all)...); err != nil {
					t.Fatalf("exec the statement that empties the children: %v", err)
				}
			}
			before := map[string]int64{}
			for _, name := range retained {
				before[name] = countIn(t, f, tx, tc.table, ids[name])
			}
			result, err := tx.ExecContext(f.ctx, tc.statement, deleteArgs(tc.statement, all)...)
			if err != nil {
				t.Fatalf("exec: %v", err)
			}
			if got, _ := result.RowsAffected(); got != tc.want {
				t.Errorf("%s delete removed %d rows from an array of every generation, want %d (e2, e5 and e8 only, and a generation row only once its children are gone)", tc.name, got, tc.want)
			}
			for _, name := range retained {
				if got := countIn(t, f, tx, tc.table, ids[name]); got != before[name] {
					t.Errorf("%s delete changed %s rows of %s from %d to %d, a generation Rule A keeps", tc.name, name, tc.table, before[name], got)
				}
			}
		})
	}
}

// TestGenerationRowDeleteGuardsItselfWithoutChildrenLive calls the generation row
// delete on generation rows that have no evidence and no candidate rows, so the
// two child tests have nothing to block on and cannot hide the statement's own
// predicates (TestDeleteStatementsGuardThemselvesLive keeps rows on every
// retained generation, and those rows alone keep them whole). Each retained row
// is held back by exactly one predicate: its status, a scope_generations row, or a
// scope pointer. Only the plain orphan may go.
func TestGenerationRowDeleteGuardsItselfWithoutChildrenLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-gd-active")
	f.generation(t, "scope-gd-active", "gen-gd-active", "active")
	f.scope(t, "scope-gd-sg")
	f.scopeGeneration(t, "scope-gd-sg", "gen-gd-sg", "superseded")
	f.generation(t, "scope-gd-sg", "gen-gd-sg", "superseded")
	f.scope(t, "scope-gd-ptr")
	f.generation(t, "scope-gd-ptr", "gen-gd-ptr", "superseded")
	f.pointer(t, "scope-gd-ptr", "gen-gd-ptr")
	f.scope(t, "scope-gd-orphan")
	f.generation(t, "scope-gd-orphan", "gen-gd-orphan", "superseded")

	cases := []struct {
		id   string
		want int64 // generation rows left after the delete
		why  string
	}{
		{"gen-gd-active", 1, "its row is active (status)"},
		{"gen-gd-sg", 1, "a scope_generations row names it"},
		{"gen-gd-ptr", 1, "a scope's active pointer names it"},
		{"gen-gd-orphan", 0, "nothing keeps it: the only row the delete may remove"},
	}
	ids := make([]string, 0, len(cases))
	for _, tc := range cases {
		if got := f.of(t, tc.id); got != (counts{generations: 1}) {
			t.Fatalf("%s seeded as %+v, want one generation row and no children", tc.id, got)
		}
		ids = append(ids, tc.id)
	}
	result, err := f.db.ExecContext(f.ctx, relationshipretention.DeleteGenerationsSQL, ids)
	if err != nil {
		t.Fatalf("delete generations: %v", err)
	}
	if got, _ := result.RowsAffected(); got != 1 {
		t.Errorf("the generation row delete removed %d rows from four childless generations, want 1 (the plain orphan)", got)
	}
	for _, tc := range cases {
		if got := f.of(t, tc.id).generations; got != tc.want {
			t.Errorf("%s has %d generation rows after the delete, want %d (%s)", tc.id, got, tc.want, tc.why)
		}
	}
}

// TestSettleReadsTheGenerationRowLive pins the settle read's third branch. A
// claimed generation whose only remaining row is its generation row (a writer's
// row the generation row delete skipped, held by another session) must come back
// from the read, or the batch would count it swept; a generation with nothing left
// must not. The child tables have no row for either id, so the other two branches
// cannot answer for it.
func TestSettleReadsTheGenerationRowLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-settle-row")
	f.generation(t, "scope-settle-row", "gen-settle-row", "superseded")
	rows, err := f.db.QueryContext(f.ctx, relationshipretention.SettleSQL, []string{"gen-settle-row", "gen-settle-nothing"})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var left []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		left = append(left, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("settle rows: %v", err)
	}
	if len(left) != 1 || left[0] != "gen-settle-row" {
		t.Fatalf("settle returned %v, want only gen-settle-row: its generation row is the one row left", left)
	}
}

// deleteArgs is the argument list a delete statement takes: the ids, and for the
// two statements that pick rows the production row limit.
func deleteArgs(statement string, ids []string) []any {
	if statement == relationshipretention.DeleteEvidenceSQL || statement == relationshipretention.DeleteCandidatesSQL {
		return []any{ids, relationshipretention.BatchRows}
	}
	return []any{ids}
}

// countIn counts one generation's rows in a table inside the caller's transaction.
func countIn(t *testing.T, f *liveFixture, tx *sql.Tx, table, generationID string) int64 {
	t.Helper()
	var n int64
	if err := tx.QueryRowContext(f.ctx, fmt.Sprintf(`SELECT count(*) FROM %s WHERE generation_id = $1`, table), generationID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestSweepStopsAtTheRowCapLive pins the bounded batch against real rows: with
// two generations per batch and a 40-row cap, a pass commits batches of 32 rows
// until the cap is crossed (two batches, four generations), reports Capped=rows,
// and leaves the other generations untouched for the next pass.
func TestSweepStopsAtTheRowCapLive(t *testing.T) {
	f := openLiveFixture(t)
	f.scope(t, "scope-cap")
	const generations = 12
	for i := 0; i < generations; i++ {
		id := fmt.Sprintf("gen-cap-%02d", i)
		f.generation(t, "scope-cap", id, "superseded")
		f.evidence(t, id, 10)
		f.candidates(t, id, 5)
	}
	opts := relationshipretention.Options{Deletes: true, RowCap: 40, BatchGenerations: 2}
	result, err := relationshipretention.Sweep(f.ctx, f.database, opts)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if result.Batches != 2 || result.GenerationsSwept != 4 || result.Capped != relationshipretention.CapRows {
		t.Fatalf("pass = %d batches, %d generations, capped %q, want 2 batches, 4 generations, rows",
			result.Batches, result.GenerationsSwept, result.Capped)
	}
	if got := result.RowsSwept[relationshipretention.EvidenceTable]; got != 40 {
		t.Fatalf("evidence rows swept = %d, want 40", got)
	}
	var remaining int
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM relationship_generations WHERE scope = 'scope-cap'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != generations-4 {
		t.Fatalf("%d generation rows remain, want %d untouched", remaining, generations-4)
	}
	// The next passes finish the backlog, and the last one is clean.
	for i := 0; i < 10; i++ {
		if again, err := relationshipretention.Sweep(f.ctx, f.database, opts); err != nil {
			t.Fatalf("Sweep: %v", err)
		} else if again.Capped == "" {
			break
		}
	}
	if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM relationship_generations WHERE scope = 'scope-cap'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d generation rows remain after repeated capped passes, want a drained backlog", remaining)
	}
}
