// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/code/reachability"
)

// TestDeadCodeRunGateMatchesReachabilityLoaderGate is the #7547 drift guard
// between the two places that decide whether an acceptance run holds a
// complete code-edge set: the dead-code incoming-edge run_gate (which falls
// back to the unbound query when the run is incomplete) and the reachability
// loader (which does not schedule an incomplete run at all). The run_gate
// body is cut from the live statement deadCodeIncomingBoundQuery builds, not
// from a copy, and compared to reachabilitystore.CompleteRunGateSQL after
// mapping the dead-code aliases onto the loader's (active.is_delta ->
// generation.is_delta, active.* -> acceptance.*, $1 -> the acceptance unit).
// An edit to either gate that the other does not repeat fails here.
func TestDeadCodeRunGateMatchesReachabilityLoaderGate(t *testing.T) {
	statement := deadCodeIncomingBoundQuery("$2")
	const open, closing = "SELECT coalesce(bool_and(", "), false) AS bound"
	start := strings.Index(statement, open)
	end := strings.Index(statement, closing)
	if start < 0 || end < start {
		t.Fatalf("run_gate bool_and body not found in deadCodeIncomingBoundQuery:\n%s", statement)
	}
	runGate := statement[start+len(open) : end]
	runGate = strings.NewReplacer(
		"active.is_delta", "generation.is_delta",
		"active.", "acceptance.",
		"$1", "acceptance.acceptance_unit_id",
	).Replace(runGate)

	got := normalizeGateSQL(runGate)
	want := normalizeGateSQL(reachabilitystore.CompleteRunGateSQL)
	if got != want {
		t.Fatalf("dead-code run_gate and reachability loader gate disagree\nrun_gate: %s\nloader:   %s", got, want)
	}
}

var gateSQLSpace = regexp.MustCompile(`\s+`)

// normalizeGateSQL collapses whitespace so layout differences between the
// two Go string literals never count as drift; every token still must match.
func normalizeGateSQL(sqlText string) string {
	collapsed := gateSQLSpace.ReplaceAllString(strings.TrimSpace(sqlText), " ")
	return strings.NewReplacer("( ", "(", " )", ")").Replace(collapsed)
}
