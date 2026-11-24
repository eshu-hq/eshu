// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"regexp"
	"strings"
	"testing"
)

// winnersAdmissionOrderBy extracts the ORDER BY list of the shipped admission
// statement, whitespace-normalized. It reads the constant the tick runs, so
// the pin cannot drift from the statement.
var winnersAdmissionOrderBy = regexp.MustCompile(`(?is)\bORDER\s+BY\s+(.*?)\s+LIMIT\s+\$1\s*$`)

// TestWinnersKeyedAdmissionOrdersByCapturedAtThenSeq pins the #11075 admission
// order: the keyed tick admits the window by (captured_at, seq), so the
// oldest debt (a scope-remainder conversion keeps its old captured_at on a new
// tail seq) is consumed first and the min(captured_at) horizon can advance.
// FIFO by seq alone froze the watermark on the QA environment.
func TestWinnersKeyedAdmissionOrdersByCapturedAtThenSeq(t *testing.T) {
	t.Parallel()

	m := winnersAdmissionOrderBy.FindStringSubmatch(admitWinnersKeyedWindowSQL)
	if m == nil {
		t.Fatalf("admission statement has no trailing ORDER BY ... LIMIT $1:\n%s", admitWinnersKeyedWindowSQL)
	}
	if got, want := strings.Join(strings.Fields(m[1]), " "), "captured_at, seq"; got != want {
		t.Errorf("admission ORDER BY = %q, want %q (oldest capture first, seq as the unique tie-break)", got, want)
	}
	if n := strings.Count(strings.ToUpper(admitWinnersKeyedWindowSQL), "ORDER BY"); n != 1 {
		t.Errorf("admission statement has %d ORDER BY clauses, want exactly 1", n)
	}
	if !strings.Contains(admitWinnersKeyedWindowSQL, "FROM supply_chain_impact_winners_dirty_log") {
		t.Errorf("admission statement no longer reads the dirty log:\n%s", admitWinnersKeyedWindowSQL)
	}
}
