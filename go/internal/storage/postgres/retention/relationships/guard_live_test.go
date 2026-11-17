// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10823 disposable PostgreSQL 18 proof that every relationship retention delete guards itself and the batch is bounded: each of the evidence, candidate and generation row deletes, called directly with an array holding every E1 to E8 generation (the ids a wrong caller could pass), removes only the rows of generations that satisfy Rule A and leaves the retained, stuck-active, pointer-named, pending and refinalize generations whole; the generation row delete removes nothing while a generation still has evidence rows or candidate rows, each kind checked on its own; and a pass with a small batch size and a row cap stops after the batch that crosses the cap and leaves the rest for the next pass
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
