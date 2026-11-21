// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestWinnersKeyedTempIndexesCoverTheTickTempTables pins the capacity-lane
// contract (#10839): every temp table the keyed tick joins in convert and the
// applied delete carries a temp index plus an ANALYZE, so the per-tick scope
// expansion plans stay deterministic instead of statistics-blind nested loops.
// The indexes are session-local to the tick transaction, like the tables.
func TestWinnersKeyedTempIndexesCoverTheTickTempTables(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		table, indexSQL, analyzeSQL string
	}{
		{winnersKeyedConsumedTable, indexWinnersKeyedConsumedSeqSQL, analyzeWinnersKeyedConsumedSQL},
		{winnersKeyedScopeIdentsTable, indexWinnersKeyedScopeIdentsSQL, analyzeWinnersKeyedScopeIdentsSQL},
		{winnersKeyedChunkIdentsTable, indexWinnersKeyedChunkIdentsSQL, analyzeWinnersKeyedChunkIdentsSQL},
	} {
		if !strings.Contains(tc.indexSQL, "ON "+tc.table+" ") {
			t.Errorf("tick temp index for %q does not index that table: %.80s", tc.table, strings.TrimSpace(tc.indexSQL))
		}
		if strings.TrimSpace(tc.analyzeSQL) != "ANALYZE "+tc.table {
			t.Errorf("tick temp analyze for %q = %q, want exactly ANALYZE %s", tc.table, tc.analyzeSQL, tc.table)
		}
	}
}

// TestWinnersKeyedBuildIdentsStatementOrder pins the build_idents phase: the
// scope expansion is created, indexed, and analyzed before the identifier
// table reads it, so the identifier join and every later consumer plan on
// real statistics.
func TestWinnersKeyedBuildIdentsStatementOrder(t *testing.T) {
	t.Parallel()

	position := map[string]int{}
	for i, stmt := range winnersKeyedBuildIdentsStmts {
		position[stmt] = i
	}
	ordered := []string{
		buildWinnersKeyedScopeIdentsSQL,
		indexWinnersKeyedScopeIdentsSQL,
		analyzeWinnersKeyedScopeIdentsSQL,
		buildWinnersKeyedIdentsSQL,
		analyzeWinnersKeyedIdentsSQL,
	}
	if len(winnersKeyedBuildIdentsStmts) != len(ordered) {
		t.Fatalf("build_idents runs %d statements, want %d", len(winnersKeyedBuildIdentsStmts), len(ordered))
	}
	for i, stmt := range ordered {
		got, ok := position[stmt]
		if !ok {
			t.Errorf("build_idents is missing statement %d", i)
			continue
		}
		if got != i {
			t.Errorf("build_idents runs statement %d at position %d", i, got)
		}
	}
}

// TestWinnersTempTableDDLNamesMatchTheTableConstants pins the literal temp
// table names in the CREATE statements to the constants the rest of the
// keyed and audit SQL reference. The DDL parity guard cannot read a table
// name joined in at build time, so the statements spell the names out; this
// test keeps the two spellings from drifting apart.
func TestWinnersTempTableDDLNamesMatchTheTableConstants(t *testing.T) {
	t.Parallel()

	cases := []struct{ sql, table string }{
		{createWinnersKeyedConsumedSQL, winnersKeyedConsumedTable},
		{buildWinnersKeyedIdentsSQL, winnersKeyedIdentsTable},
		{buildWinnersKeyedActiveFactsSQL, winnersKeyedActiveFactsTable},
		{createWinnersAuditCandidatesSQL, winnersAuditCandidatesTable},
		{seedWinnersAuditIdentsSQL, winnersKeyedIdentsTable},
		{createWinnersAuditStaleSQL, winnersAuditStaleTable},
		{createWinnersAuditPendingSQL, winnersAuditPendingTable},
		{createWinnersAuditRangePendingSQL, winnersAuditRangePendingTable},
		{createWinnersAuditRangeStalePendingSQL, winnersAuditPendingTable},
	}
	for _, c := range cases {
		if !strings.Contains(c.sql, "CREATE TEMP TABLE "+c.table+" ") {
			t.Errorf("statement does not create temp table %q: %.80s", c.table, strings.TrimSpace(c.sql))
		}
	}
}
