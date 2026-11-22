// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// staleGate11234Statement is one statement the differential runs in its
// pre-#11234 and shipped forms.
type staleGate11234Statement struct {
	name  string
	query string
	args  func(domains any) []any
}

// staleGate11234Statements lists every statement that composes the gate:
// the gate's stale set itself, the batch claim, the single claim, and the
// funnel census. The pre-#11234 form of each is the shipped text with the
// gate swapped back to the frozen base text (staleGate11234PreGate).
func staleGate11234Statements(now time.Time) []staleGate11234Statement {
	claimArgs := func(domains any) []any {
		return []any{now, domains, "diff-11234", now.Add(time.Minute), false, 0, 0}
	}
	return []staleGate11234Statement{
		{
			name: "stale_set",
			query: "\nWITH " + reducerClaimReadinessRequirementsCTE() + ",\n" + reducerClaimStaleGateCTEs() +
				"\nSELECT work_item_id FROM stale_reducer_candidates WHERE ($1::timestamptz IS NULL OR $1::timestamptz IS NOT NULL)",
			args: func(domains any) []any { return []any{now, domains} },
		},
		{name: "batch_claim", query: claimReducerWorkBatchQuery, args: func(domains any) []any { return append(claimArgs(domains), 100000) }},
		{name: "single_claim", query: claimReducerWorkQuery, args: claimArgs},
		{name: "funnel", query: reducerClaimBatchFunnelQuery, args: claimArgs},
	}
}

// staleGate11234Rows runs query inside a rolled-back transaction and
// returns every row rendered as text, sorted: the claim's UPDATE never
// commits, so both forms see the same snapshot of the seed. A statement
// that returns work_item_id is compared on that column alone (the hand-out
// set); the claim's lease timestamps differ between any two runs.
func staleGate11234Rows(ctx context.Context, t testing.TB, conn *sql.Conn, query string, args []any) []string {
	t.Helper()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	idColumn := -1
	for i, col := range cols {
		if col == "work_item_id" {
			idColumn = i
		}
	}
	var out []string
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		var parts []string
		for i, v := range values {
			if idColumn >= 0 && i != idColumn {
				continue
			}
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			parts = append(parts, fmt.Sprintf("%s=%v", cols[i], v))
		}
		out = append(out, strings.Join(parts, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	sort.Strings(out)
	return out
}

// staleGate11234Except returns the rows of a missing from b.
func staleGate11234Except(a, b []string) []string {
	seen := map[string]int{}
	for _, row := range b {
		seen[row]++
	}
	var missing []string
	for _, row := range a {
		if seen[row] > 0 {
			seen[row]--
			continue
		}
		missing = append(missing, row)
	}
	return missing
}

// TestStaleGate11234DifferentialLive is the #11234 differential: on one
// snapshot of the QA shape plus every edge seed (seedStaleGate11234Edges),
// with 0 and 200 stale rows and the domain filter NULL and set, the
// pre-#11234 and shipped forms of every statement that composes the gate
// return identical rows (EXCEPT empty both ways): the stale set, the batch
// claim's hand-out, the single claim's hand-out, and the funnel counts.
func TestStaleGate11234DifferentialLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Minute)
	defer cancel()
	pre := staleGate11234PreGate(t)
	for _, staleRows := range []int{0, 200} {
		t.Run(fmt.Sprintf("stale=%d", staleRows), func(t *testing.T) {
			sqlConn, conn := openStaleGate11234Live(ctx, t, fmt.Sprintf("diff%d", staleRows), true)
			shape := staleGate11234QAShape()
			shape.staleRows = staleRows
			seedStaleGate11234(ctx, t, conn, shape)
			now := seedStaleGate11234Edges(ctx, t, conn)
			if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
				t.Fatalf("analyze: %v", err)
			}
			for _, domains := range []any{nil, staleGate11234EdgeDomains()} {
				for _, stmt := range staleGate11234Statements(now) {
					shipped := staleGate11234Rows(ctx, t, sqlConn, stmt.query, stmt.args(domains))
					before := staleGate11234Rows(ctx, t, sqlConn, staleGate11234SwapGate(t, stmt.query, pre), stmt.args(domains))
					if extra := staleGate11234Except(shipped, before); len(extra) > 0 {
						t.Errorf("%s domains=%v: shipped EXCEPT pre-#11234 = %d rows, first %q", stmt.name, domains != nil, len(extra), extra[0])
					}
					if lost := staleGate11234Except(before, shipped); len(lost) > 0 {
						t.Errorf("%s domains=%v: pre-#11234 EXCEPT shipped = %d rows, first %q", stmt.name, domains != nil, len(lost), lost[0])
					}
					t.Logf("%s domains=%v rows=%d", stmt.name, domains != nil, len(shipped))
					if stmt.name == "stale_set" {
						staleGate11234AssertEdgeVerdicts(t, shipped, domains != nil)
					}
				}
			}
		})
	}
}
