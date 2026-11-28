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
		buildWinnersKeyedScopeIdentsFromSupportsSQL,
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

// TestWinnersFullChunkTempDropMatchesTheTableConstants pins the full walk's
// single-lifecycle DROP to the temp tables it owns: a renamed table the
// DROP still names old would leave the keyed seven-column identifier table
// behind, and the walk's CREATE would fail with 42P07 on the fallback path.
func TestWinnersFullChunkTempDropMatchesTheTableConstants(t *testing.T) {
	t.Parallel()

	if !strings.HasPrefix(strings.TrimSpace(dropWinnersFullChunkTempSQL), "DROP TABLE IF EXISTS") {
		t.Fatalf("full-chunk temp lifecycle is not a guarded drop: %.120s", strings.TrimSpace(dropWinnersFullChunkTempSQL))
	}
	for _, table := range []string{
		// The enumeration table has no constant (the CREATE/ANALYZE spell
		// it literally, like the DDL parity scan reads); the rest reuse
		// the keyed table constants the walk shares with the tick.
		"winners_full_idents", winnersKeyedIdentsTable, winnersKeyedNarrowTable, winnersKeyedActiveFactsTable,
	} {
		if !strings.Contains(dropWinnersFullChunkTempSQL, table) {
			t.Errorf("full-chunk DROP does not drop temp table %q: %.120s", table, dropWinnersFullChunkTempSQL)
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
		{buildWinnersKeyedNarrowSQL, winnersKeyedNarrowTable},
		{buildWinnersKeyedActiveFactsSQL, winnersKeyedActiveFactsTable},
		{buildWinnersKeyedActiveFactsNarrowFromSupportsSQL, winnersKeyedActiveFactsTable},
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
