// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

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
	}
	for _, c := range cases {
		if !strings.Contains(c.sql, "CREATE TEMP TABLE "+c.table+" ") {
			t.Errorf("statement does not create temp table %q: %.80s", c.table, strings.TrimSpace(c.sql))
		}
	}
}
