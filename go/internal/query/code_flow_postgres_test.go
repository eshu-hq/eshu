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

// TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex guards the #5280
// fix: the read filters fact_kind by both the parameterized $1 subset AND a
// literal `fact_kind IN (...)` conjunct. The literal is what lets PostgreSQL
// prove the fact_records_code_flow_repo_idx partial-index predicate at plan
// time; without it a generic prepared plan cannot prove $1 is limited to these
// kinds and silently falls back to the all-scope over-fetch (proven under
// plan_cache_mode=force_generic_plan: 10,137 buffers vs 728 with the conjunct).
// Removing this line reverts the index win on the real pgx production path.
func TestCodeFlowSQLKeepsLiteralKindConjunctForPartialIndex(t *testing.T) {
	t.Parallel()

	query := listActiveCodeFlowFactsSQL
	if !strings.Contains(query, "fact.fact_kind = ANY($1::text[])") {
		t.Fatal("code-flow SQL must keep the parameterized $1 kind filter for the exact per-read subset")
	}
	if !strings.Contains(query, "fact.fact_kind IN ('code_taint_evidence', 'code_interproc_evidence', 'code_dataflow_function')") {
		t.Fatal("code-flow SQL must keep the literal fact_kind IN (...) conjunct so a generic prepared plan can use the partial index fact_records_code_flow_repo_idx (#5280)")
	}
	// The literal set must cover every fact kind codeFlowFactKinds can return
	// across all CodeFlowKind values, so the conjunct is redundant
	// (result-neutral) rather than a filter that could drop rows the $1 subset
	// would have returned.
	for _, kind := range []CodeFlowKind{
		CodeFlowKindTaintPath, CodeFlowKindReachingDef,
		CodeFlowKindCFGSummary, CodeFlowKindPDGSummary,
	} {
		for _, k := range codeFlowFactKinds(kind) {
			if !strings.Contains(query, "'"+k+"'") {
				t.Fatalf("code-flow SQL literal conjunct is missing kind %q that codeFlowFactKinds(%q) can select; the conjunct must cover every selectable kind or it would drop rows", k, kind)
			}
		}
	}
}
