// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// latestGenerationCTEQueries is the set of production queries that select the
// latest active generation per scope through the shared latestGenerationCTE. The
// #3704 long-pole fix rewrote that CTE from a per-scope correlated subquery
// (ORDER BY ... LIMIT 1 evaluated once per GROUP BY row, which the planner
// grossly under-estimates) into a single DISTINCT ON pass. Every query that
// embeds the CTE must use the rewritten shape so none retains the misestimated
// correlated subplan.
var latestGenerationCTEQueries = map[string]string{
	"listLatestRelationshipFactRecordsQuery":              listLatestRelationshipFactRecordsQuery,
	"activeRepositoryGenerationsQuery":                    activeRepositoryGenerationsQuery,
	"activeScopeGenerationPartitionsQuery":                activeScopeGenerationPartitionsQuery,
	"listOnboardedRepoScopedRelationshipFactRecordsQuery": listOnboardedRepoScopedRelationshipFactRecordsQuery,
	"listDeferredScopedRelationshipFactRecordsQuery":      listDeferredScopedRelationshipFactRecordsQuery,
	"resolveRepoActiveGenerationsQuery":                   resolveRepoActiveGenerationsQuery,
	"listArgoCDGeneratorConfigFactRecordsQuery":           listArgoCDGeneratorConfigFactRecordsQuery,
}

// TestLatestGenerationCTEHasNoCorrelatedSubquery is the #3704 regression gate.
// The latest-generation selection must not re-introduce the per-scope correlated
// subquery (the `SELECT generation_id ... WHERE candidate.scope_id =
// generation.scope_id ORDER BY ... LIMIT 1` form). That subplan was evaluated
// once per scope_generations group and the planner under-estimated its
// cardinality, producing the corpus-wide CPU-bound long pole. The DISTINCT ON
// rewrite is a single sorted pass, so the correlated marker must be absent and
// the DISTINCT ON marker present in every query that embeds the CTE.
func TestLatestGenerationCTEHasNoCorrelatedSubquery(t *testing.T) {
	t.Parallel()

	const correlatedMarker = "WHERE candidate.scope_id = generation.scope_id"
	for name, query := range latestGenerationCTEQueries {
		if strings.Contains(query, correlatedMarker) {
			t.Errorf("%s still embeds the correlated latest-generation subquery (%q); it must use the DISTINCT ON rewrite", name, correlatedMarker)
		}
		if !strings.Contains(query, "DISTINCT ON (generation.scope_id)") {
			t.Errorf("%s must select the latest generation with DISTINCT ON (generation.scope_id)", name)
		}
	}
}

// TestLatestGenerationCTEPreservesActiveGenerationPreference pins the truth-
// preserving COALESCE: the active scope pointer (ingestion_scopes.active_generation_id)
// still wins over the newest-by-ingested-time generation. The rewrite must keep
// the COALESCE(scope.active_generation_id, ...) precedence so evidence attaches
// to exactly the generation the correlated form selected.
func TestLatestGenerationCTEPreservesActiveGenerationPreference(t *testing.T) {
	t.Parallel()

	for name, query := range latestGenerationCTEQueries {
		if !strings.Contains(query, "COALESCE(") || !strings.Contains(query, "scope.active_generation_id") {
			t.Errorf("%s must keep COALESCE(scope.active_generation_id, latest) generation precedence", name)
		}
		if !strings.Contains(query, "ORDER BY generation.scope_id, generation.ingested_at DESC, generation.generation_id DESC") {
			t.Errorf("%s DISTINCT ON must order by (scope_id, ingested_at DESC, generation_id DESC) to pick the newest generation deterministically", name)
		}
	}
}

// TestScopeGenerationsLatestLookupIndexExists pins the covering index that backs
// the DISTINCT ON latest-generation sort. The DISTINCT ON orders each scope's
// generations by (ingested_at DESC, generation_id DESC); a leading
// (scope_id, ingested_at DESC, generation_id DESC) index lets Postgres satisfy
// the per-scope ordering without a full sort of scope_generations.
func TestScopeGenerationsLatestLookupIndexExists(t *testing.T) {
	t.Parallel()

	var generations Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "scope_generations" {
			generations = def
			break
		}
	}
	if generations.Name == "" {
		t.Fatal("scope_generations definition missing")
	}
	for _, want := range []string{
		"scope_generations_scope_latest_lookup_idx",
		"(scope_id, ingested_at DESC, generation_id DESC)",
	} {
		if !strings.Contains(generations.SQL, want) {
			t.Fatalf("scope_generations SQL missing %q for the latest-generation DISTINCT ON lookup", want)
		}
	}
}
