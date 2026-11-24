// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// TestAdvisoryGroupWinnersReadPayloadFromSupports is the #10880 PR-C2 regression for the one reader of
// finding payloads that is neither a route nor an audit: the advisory group winners read resolves each
// winner's purl and group members through winner_fact_id. Since PR-C2 the impact writer writes no finding
// fact, so a join on fact_records alone returns NULL for every new winner: the purl is empty and the
// provenance members are lost, and the AdvisoryGroup node silently shrinks to its own CVE. The payload
// must come from the supports table (the winner's fact id is the supports primary key), with fact_records
// as the fallback only for a winner whose support the backfill has not yet copied.
func TestAdvisoryGroupWinnersReadPayloadFromSupports(t *testing.T) {
	t.Parallel()

	query := advisoryGroupWinnersQuery
	if !strings.Contains(query, "LEFT JOIN supply_chain_impact_finding_supports AS s\n  ON s.fact_id = w.winner_fact_id") {
		t.Fatalf("the winners read must join the supports table on the winner fact id:\n%s", query)
	}
	if !strings.Contains(query, "LEFT JOIN fact_records AS f\n  ON f.fact_id = w.winner_fact_id") {
		t.Fatalf("the winners read keeps the fact_records fallback for a support the backfill has not copied:\n%s", query)
	}
	for _, expression := range []string{
		"COALESCE(" + advisoryGroupWinnerPayloadSQL + "->>'purl', '')",
		advisoryGroupWinnerPayloadSQL + " #> '{provenance,advisory_group_members}'",
	} {
		if !strings.Contains(query, expression) {
			t.Fatalf("the winners read must extract through the supports-first payload, missing %q:\n%s", expression, query)
		}
	}
	if advisoryGroupWinnerPayloadSQL != "COALESCE(s.payload, f.payload)" {
		t.Fatalf("payload source = %q, want the supports payload first and the fact payload as the fallback", advisoryGroupWinnerPayloadSQL)
	}
}
