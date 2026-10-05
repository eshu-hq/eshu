// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestActiveWorkSummaryOracleIsThePinnedBaseline keeps the grouped-history
// differential's oracle equal to the SQL the #7009 C3 shim measured.
func TestActiveWorkSummaryOracleIsThePinnedBaseline(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(activeWorkSummaryPreHistoryGroupsOracle))
	if got := hex.EncodeToString(sum[:]); got != activeWorkSummaryPreHistoryGroupsOracleSHA256 {
		t.Fatalf("oracle sha256 = %s, want %s", got, activeWorkSummaryPreHistoryGroupsOracleSHA256)
	}
}

// TestActiveWorkSummaryGroupsHistoryBeforeTheGenerationJoin pins the #7009
// grouped-history shape: the wide detail CTE reads only the six statuses a
// section reads in detail, every other row is counted by one narrow
// (scope_id, generation_id, stage, status) pass joined to generation identity
// per group, and total_count comes from that same pass.
func TestActiveWorkSummaryGroupsHistoryBeforeTheGenerationJoin(t *testing.T) {
	t.Parallel()

	query := activeWorkSummaryQuery
	for name, want := range map[string]string{
		"detail statuses": "FROM (SELECT * FROM fact_work_items WHERE " + historyDetailSix + ") AS work",
		"grouped pass":    "fact_work_status_groups AS MATERIALIZED (\n  SELECT scope_id, generation_id, stage, status, COUNT(*) AS row_count\n  FROM fact_work_items\n  GROUP BY scope_id, generation_id, stage, status\n)",
		"history fence":   historyJoinKey + "\n  WHERE " + historyHistorySix,
		"total pass":      historyGroupsTotal,
		"succeeded":       historySucceededSource,
		"stage merge":     "SELECT stage, status, row_count FROM fact_work_history_counts",
	} {
		if strings.Count(query, want) != 1 {
			t.Errorf("%s: want exactly one %q in the summary query", name, want)
		}
	}
	if strings.Contains(query, "(SELECT COUNT(*) FROM fact_work_items)") {
		t.Errorf("summary query still counts fact_work_items in a second pass")
	}
	order := []string{
		"\nactive_fact_work_items AS MATERIALIZED (",
		"\nfact_work_status_groups AS MATERIALIZED (",
		"\nfact_work_history_counts AS MATERIALIZED (",
		"\nfact_domain_backlogs AS (",
	}
	last := -1
	for _, name := range order {
		at := strings.Index(query, name)
		if at <= last {
			t.Fatalf("summary query must define %q after the previous CTE (at %d, previous %d)", name, at, last)
		}
		last = at
	}
}

// TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups proves the #7009
// rewrite changed nothing but the grouped-history pieces: undoing exactly
// those pieces, with the still-shipped standalone selects, must reproduce the
// pre-change oracle byte for byte.
func TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups(t *testing.T) {
	t.Parallel()

	query := activeWorkSummaryQuery
	for _, pair := range [][2]string{
		{activeWorkSummaryHistoryCTEs + ",\n", ""},
		{activeWorkSummaryStageCountsSelect, stageCountsSelect},
		{activeWorkSummaryQueueSelect, queueSnapshotSelect},
		{activeWorkSummaryWorkInput, "FROM (SELECT * FROM fact_work_items OFFSET 0) AS work"},
	} {
		if n := strings.Count(query, pair[0]); n != 1 {
			t.Fatalf("summary query contains %d copies of %q, want 1", n, pair[0])
		}
		query = strings.Replace(query, pair[0], pair[1], 1)
	}
	if query != activeWorkSummaryPreHistoryGroupsOracle {
		t.Fatalf("undoing the grouped history pieces does not reproduce the oracle:\n%s", query)
	}
}
