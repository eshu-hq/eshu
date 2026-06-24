// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestStatusQueriesExcludeInactiveReducerGenerationsFromLiveReadiness(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"stageCountsQuery":        stageCountsQuery,
		"domainBacklogQuery":      domainBacklogQuery,
		"queueSnapshotQuery":      queueSnapshotQuery,
		"latestQueueFailureQuery": latestQueueFailureQuery,
	} {
		for _, want := range inactiveReducerGenerationPredicateFragments() {
			if !strings.Contains(query, want) {
				t.Fatalf("%s missing inactive-generation live-readiness predicate %q:\n%s", name, want, query)
			}
		}
	}

	if !strings.Contains(queueSnapshotQuery, "(SELECT COUNT(*) FROM fact_work_items) AS total_count") {
		t.Fatalf("queueSnapshotQuery must preserve audit total while excluding stale rows from live readiness:\n%s", queueSnapshotQuery)
	}
}

func inactiveReducerGenerationPredicateFragments() []string {
	return []string{
		"active_fact_work_items AS (",
		"FROM fact_work_items AS work",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = active_generation.generation_id",
		"work.stage = 'reducer'",
		"work.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"FROM active_fact_work_items",
	}
}
