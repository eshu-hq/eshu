// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codetopicparallel"
)

// entityLateralSection extracts the per-term CROSS JOIN LATERAL body from a
// code-topic statement so shape pins do not depend on CTE layout.
func entityLateralSection(t *testing.T, query string) string {
	t.Helper()
	start := strings.Index(query, "CROSS JOIN LATERAL (")
	if start == -1 {
		if start = strings.Index(query, "CROSS JOIN LATERAL("); start == -1 {
			t.Fatalf("query has no CROSS JOIN LATERAL probe: %q", query)
		}
	}
	depth := 0
	for i := start; i < len(query); i++ {
		switch query[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return query[start : i+1]
			}
		}
	}
	t.Fatalf("unbalanced CROSS JOIN LATERAL probe: %q", query)
	return ""
}

// normalizeLateralWhitespace collapses every whitespace run so the serial
// and parallel lateral twins compare by logic, not layout.
func normalizeLateralWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TestInvestigateCodeTopicEntityProbeSplitsNameFirst pins the #7033 tail fix:
// the entity lateral probes the narrow entity_name column first and reads the
// TOASTed source_cache only for the remaining per-term budget, excluding rows
// the name pool already holds. Four terms make the fixed 4,000-row pool budget
// a 1,000-row per-term cap.
func TestInvestigateCodeTopicEntityProbeSplitsNameFirst(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms",
			"score", "pool_truncated",
		},
	}})
	if _, err := NewContentReader(db).InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		Terms: []string{"config", "content", "deploy", "system"}, Limit: 26,
	}); err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v", err)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("statements = %d, want 1", got)
	}
	probe := entityLateralSection(t, recorder.queries[0])

	// One name pool per lateral definition; the TOASTed source probe runs only
	// for the remaining capacity and never re-emits a name hit.
	if got, want := strings.Count(probe, "name_pool AS MATERIALIZED"), 1; got != want {
		t.Fatalf("name pool count = %d, want %d (one materialized name pool per lateral)", got, want)
	}
	if got, want := strings.Count(probe, "source_pool AS ("), 1; got != want {
		t.Fatalf("source pool count = %d, want %d (one count-gated source pool per lateral)", got, want)
	}
	if got, want := strings.Count(probe, "e.entity_name NOT ILIKE"), 1; got != want {
		t.Fatalf("name exclusion count = %d, want %d (source probe must exclude name hits)", got, want)
	}
	if got, want := strings.Count(probe, "LIMIT (SELECT 1000 - count(*) FROM name_pool)"), 1; got != want {
		t.Fatalf("remaining-capacity limit count = %d, want %d (one per lateral)", got, want)
	}
	if strings.Contains(probe, "OR e.source_cache ILIKE") {
		t.Fatalf("lateral = %q, want no unbounded name-OR-source scan", probe)
	}
	if got, want := strings.Count(probe, "UNION ALL"), 1; got != want {
		t.Fatalf("lateral UNION ALL count = %d, want %d (name pool plus source pool)", got, want)
	}
}

// TestCodeTopicParallelProbeSplitsEntityNameFirst pins the same #7033 split
// on the parallel partition builder: four terms per 16-term request share one
// lateral definition per partition probe.
func TestCodeTopicParallelProbeSplitsEntityNameFirst(t *testing.T) {
	t.Parallel()

	req := CodeTopicInvestigationRequest{Terms: []string{"config", "content", "deploy", "system"}}
	filters, baseArgs, _ := codeTopicFilters(req)
	probeSQL, _ := codetopicparallel.ProbeSQL(req, codeTopicCandidateCap(len(req.Terms)), filters, baseArgs)
	probe := entityLateralSection(t, probeSQL)

	if got, want := strings.Count(probe, "name_pool AS MATERIALIZED"), 1; got != want {
		t.Fatalf("parallel name pool count = %d, want %d", got, want)
	}
	if got, want := strings.Count(probe, "source_pool AS ("), 1; got != want {
		t.Fatalf("parallel source pool count = %d, want %d", got, want)
	}
	if got, want := strings.Count(probe, "e.entity_name NOT ILIKE"), 1; got != want {
		t.Fatalf("parallel name exclusion count = %d, want %d", got, want)
	}
	if got, want := strings.Count(probe, "LIMIT (SELECT 1000 - count(*) FROM name_pool)"), 1; got != want {
		t.Fatalf("parallel remaining-capacity limit count = %d, want %d", got, want)
	}
	if strings.Contains(probe, "OR e.source_cache ILIKE") {
		t.Fatalf("parallel lateral = %q, want no unbounded name-OR-source scan", probe)
	}
}

// TestCodeTopicEntityLateralMatchesParallelLateral pins the twin builders
// together: the serial fallback lateral must stay logically identical to the
// parallel partition lateral, so a pool-accounting or predicate edit lands in
// both or fails here.
func TestCodeTopicEntityLateralMatchesParallelLateral(t *testing.T) {
	t.Parallel()

	terms := []string{"config", "content", "deploy", "system"}
	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{{
		columns: []string{
			"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
			"entity_type", "language", "start_line", "end_line", "matched_terms",
			"score", "pool_truncated",
		},
	}})
	if _, err := NewContentReader(db).InvestigateCodeTopic(context.Background(), CodeTopicInvestigationRequest{
		Terms: terms, Limit: 26,
	}); err != nil {
		t.Fatalf("InvestigateCodeTopic() error = %v", err)
	}
	serial := normalizeLateralWhitespace(entityLateralSection(t, recorder.queries[0]))

	req := CodeTopicInvestigationRequest{Terms: terms}
	filters, baseArgs, _ := codeTopicFilters(req)
	probeSQL, _ := codetopicparallel.ProbeSQL(req, codeTopicCandidateCap(len(terms)), filters, baseArgs)
	parallel := normalizeLateralWhitespace(entityLateralSection(t, probeSQL))

	// The serial builder carries the caller grant filter inside the lateral;
	// the parallel probe binds it per partition. Both laterals share one
	// readiness gate here (unscoped, no grant list), so strip that shared
	// predicate tail before comparing the split cores.
	stripGrant := func(s string) string {
		return strings.ReplaceAll(s, " AND eshu_require_content_substring_indexes_ready()", "")
	}
	if got, want := stripGrant(serial), stripGrant(parallel); got != want {
		t.Fatalf("entity lateral drift:\nserial:\n%s\nparallel:\n%s", serial, parallel)
	}
}
