// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
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

// activeWorkSummaryQuerySHA256 pins the shipped #7009 gated summary text
// (S5 variant a2v plus the mode section). Without the mode section it is the
// S5 shim's s5_a2v_T.sql byte for byte
// (TestActiveWorkSummaryIsTheMeasuredA2vRender). The ops-qa same-snapshot
// compare packet refuses to run when its text hashes differently (#7009
// arbiter ruling S4 D3 C1, S5 D5.4). Any edit to the statement must re-pin
// this with the re-rendered SHA-256 and rerun the oracle differential.
const activeWorkSummaryQuerySHA256 = "9d833808212a48859e1d5a26a9fd1c378f4bc366e6521876d7938834ccc96cae"

// TestActiveWorkSummaryQueryIsThePinnedRender keeps activeWorkSummaryQuery
// equal to the text the shim measured and the compare packet runs.
func TestActiveWorkSummaryQueryIsThePinnedRender(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte(activeWorkSummaryQuery))
	if got := hex.EncodeToString(sum[:]); got != activeWorkSummaryQuerySHA256 {
		t.Fatalf("activeWorkSummaryQuery sha256 = %s, want %s", got, activeWorkSummaryQuerySHA256)
	}
}

// TestActiveWorkSummaryGroupsHistoryBeforeTheGenerationJoin pins the #7009
// grouped-history shape: in the grouped branch the wide detail CTE reads only
// the six statuses a section reads in detail, every other row is counted by
// one narrow (scope_id, generation_id, stage, status) pass joined to
// generation identity per group, and total_count comes from that same pass;
// only the detail branch counts fact_work_items for total_count.
func TestActiveWorkSummaryGroupsHistoryBeforeTheGenerationJoin(t *testing.T) {
	t.Parallel()

	query := activeWorkSummaryQuery
	for name, want := range map[string]string{
		"detail statuses": "FROM ((SELECT * FROM fact_work_items WHERE " + historyDetailSix + "\n",
		"grouped pass":    "fact_work_status_groups AS MATERIALIZED (\n  SELECT scope_id, generation_id, stage, status, COUNT(*) AS row_count\n  FROM fact_work_items\n  WHERE (SELECT grouped FROM fact_work_summary_mode)\n  GROUP BY scope_id, generation_id, stage, status\n)",
		"history fence":   historyJoinKey + "\n  WHERE " + historyHistorySix,
		"total pass":      historyGroupsTotal,
		"succeeded":       historySucceededSource,
		"stage merge":     "SELECT stage, status, row_count FROM fact_work_history_counts",
	} {
		if strings.Count(query, want) != 1 {
			t.Errorf("%s: want exactly one %q in the summary query", name, want)
		}
	}
	if n := strings.Count(query, "(SELECT COUNT(*) FROM fact_work_items)"); n != 1 ||
		!strings.Contains(query, "ELSE (SELECT COUNT(*) FROM fact_work_items) END AS total_count") {
		t.Errorf("summary query counts fact_work_items %d times; want one count, in the detail branch's total_count", n)
	}
	order := []string{
		"\nWITH fact_work_summary_mode AS MATERIALIZED (",
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
// rewrite changed nothing but the gate, the mode section, and the
// grouped-history pieces: undoing exactly those pieces, with the
// still-shipped standalone selects, must reproduce the pre-change oracle byte
// for byte.
func TestActiveWorkSummaryDiffersFromOracleOnlyInHistoryGroups(t *testing.T) {
	t.Parallel()

	query := activeWorkSummaryQuery
	for _, pair := range [][2]string{
		{activeWorkSummaryModeCTE + ",\n", ""},
		{",\n" + activeWorkSummaryModeSection, ""},
		{activeWorkSummaryModeUnion, ""},
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

// activeWorkStatusPredicate matches one status predicate on a column named
// exactly status: its operator and its literal or literal list.
var activeWorkStatusPredicate = regexp.MustCompile(`\bstatus\s*(NOT\s+IN|IN|=|<>|!=)\s*(\([^)]*\)|'[^']*')`)

// checkSectionStatusesWithinDetail requires every status predicate a summary
// section applies, outside the grouped history CTEs and succeeded_count, to
// name only activeWorkDetailStatuses with IN or =. In the grouped branch a
// section that read any other status from active_fact_work_items would see
// no rows since #7009, because only grouped history counts them.
// succeeded_count is exempt: it sums grouped history and the detail branch's
// succeeded rows, one of which is empty. It returns the number of predicates
// checked.
func checkSectionStatusesWithinDetail(query string) (int, error) {
	sections := query
	for _, history := range []string{activeWorkSummaryHistoryCTEs, historySucceededSource, historySucceededDetail} {
		if n := strings.Count(sections, history); n != 1 {
			return 0, fmt.Errorf("summary query contains %d copies of %q, want 1", n, history)
		}
		sections = strings.Replace(sections, history, "", 1)
	}
	detail := map[string]bool{}
	for _, status := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(activeWorkDetailStatuses, -1) {
		detail[status[1]] = true
	}
	matches := activeWorkStatusPredicate.FindAllStringSubmatch(sections, -1)
	for _, m := range matches {
		if op := strings.Join(strings.Fields(m[1]), " "); op != "IN" && op != "=" {
			return 0, fmt.Errorf("section predicate %q excludes statuses; it can read history rows the detail CTE no longer holds", m[0])
		}
		for _, status := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(m[2], -1) {
			if !detail[status[1]] {
				return 0, fmt.Errorf("section predicate %q reads status %q outside activeWorkDetailStatuses %s", m[0], status[1], activeWorkDetailStatuses)
			}
		}
	}
	return len(matches), nil
}

// TestActiveWorkSummarySectionStatusesStayWithinDetailStatuses is the
// hermetic guard that every section's status filter is a subset of
// activeWorkDetailStatuses, with seeded violations that must be rejected.
func TestActiveWorkSummarySectionStatusesStayWithinDetailStatuses(t *testing.T) {
	t.Parallel()

	n, err := checkSectionStatusesWithinDetail(activeWorkSummaryQuery)
	if err != nil {
		t.Fatalf("shipped summary query: %v", err)
	}
	if n < 20 {
		t.Fatalf("checked %d status predicates, want at least 20 (the guard is not reading the sections)", n)
	}
	for name, seed := range map[string][2]string{
		"backlog widened to quarantined": {
			"\n  WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed')\n",
			"\n  WHERE status IN ('pending', 'claimed', 'running', 'retrying', 'dead_letter', 'failed', 'quarantined')\n",
		},
		"failure reads succeeded rows": {
			"WHERE status IN ('retrying', 'failed', 'dead_letter')\n  AND (",
			"WHERE status = 'succeeded'\n  AND (",
		},
		"blockage excludes instead of includes": {
			"AND status IN ('pending', 'retrying', 'claimed', 'running')\n      AND (visible_at",
			"AND status NOT IN ('failed', 'dead_letter')\n      AND (visible_at",
		},
	} {
		if c := strings.Count(activeWorkSummaryQuery, seed[0]); c != 1 {
			t.Fatalf("%s: anchor matched %d times, want 1", name, c)
		}
		if _, err := checkSectionStatusesWithinDetail(strings.Replace(activeWorkSummaryQuery, seed[0], seed[1], 1)); err == nil {
			t.Errorf("%s: seeded violation was accepted", name)
		}
	}
}
