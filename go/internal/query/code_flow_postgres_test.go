// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestCodeFlowSQLReadsLatestFactPerStableKeyThroughActiveGeneration(t *testing.T) {
	t.Parallel()

	query := listActiveCodeFlowFactsSQL
	for _, want := range []string{
		"active_scope AS",
		"active_generation.status = 'active'",
		"fact.payload->>'repo_id' = $2",
		"generation.ingested_at <= active_scope.active_ingested_at",
		"ROW_NUMBER() OVER (",
		"PARTITION BY fact.scope_id, fact.stable_fact_key",
		"ORDER BY generation.ingested_at DESC, generation.generation_id DESC, fact.observed_at DESC, fact.fact_id DESC",
		"candidate.rn = 1",
		"candidate.is_tombstone = FALSE",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("code-flow SQL missing %q", want)
		}
	}
	if strings.Contains(query, "scope.active_generation_id = fact.generation_id") {
		t.Fatal("code-flow SQL must not read only the active delta generation")
	}
}
