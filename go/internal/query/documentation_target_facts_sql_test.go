// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// legacyDocumentationTargetFactsSQL is the single-statement target-facts read
// as it stood before #7126: one kind list (including
// semantic.documentation_observation, which the partial GIN index predicate
// does not cover) in one ORDER BY ... LIMIT. It exists only as the differential
// baseline for the split read. Its WHERE conjuncts, arguments, join, and
// projection come from the same production parts the new statement uses, so a
// difference between the two statements can only be the branch structure.
func legacyDocumentationTargetFactsSQL(filter documentationFindingFilter) (string, []any) {
	parts := newDocumentationTargetFactsParts(filter)
	args := append(append([]any{}, parts.args...), parts.limit+1)
	kindClause := "fact_records.fact_kind IN ('" + facts.DocumentationEntityMentionFactKind + "', '" +
		facts.DocumentationClaimCandidateFactKind + "', '" + facts.SemanticDocumentationObservationFactKind + "')"
	clauses := append([]string{kindClause}, parts.clauses...)
	return fmt.Sprintf(`
SELECT %s AS payload
FROM fact_records%s
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT $%d
`, documentationTargetFactSelect, parts.scopeJoin, strings.Join(clauses, " AND "), len(args)), args
}

// TestBuildDocumentationTargetFactsSQLSplitsIndexedAndSemanticBranches pins the
// #7126 statement shape: the indexed kinds and the semantic kind are read by
// separate bounded branches so the indexed branch's predicate is provably
// covered by fact_records_documentation_target_refs_idx.
func TestBuildDocumentationTargetFactsSQLSplitsIndexedAndSemanticBranches(t *testing.T) {
	t.Parallel()

	query, args := buildDocumentationTargetFactsSQL(documentationFindingFilter{
		Repository: "repo:payments",
		Limit:      5,
	})

	branches := strings.Split(query, "UNION ALL")
	if len(branches) != 2 {
		t.Fatalf("statement has %d UNION ALL branches, want 2:\n%s", len(branches), query)
	}
	indexed, semantic := branches[0], branches[1]

	mention := "'" + facts.DocumentationEntityMentionFactKind + "'"
	claim := "'" + facts.DocumentationClaimCandidateFactKind + "'"
	semanticKind := "'" + facts.SemanticDocumentationObservationFactKind + "'"
	if !strings.Contains(indexed, "fact_records.fact_kind IN ("+mention+", "+claim+")") {
		t.Fatalf("indexed branch must list exactly the GIN-covered kinds:\n%s", indexed)
	}
	if strings.Contains(indexed, semanticKind) {
		t.Fatalf("indexed branch must not name the semantic kind (it is outside the GIN predicate):\n%s", indexed)
	}
	if !strings.Contains(semantic, "fact_records.fact_kind = "+semanticKind) {
		t.Fatalf("semantic branch must read the semantic kind:\n%s", semantic)
	}
	if strings.Contains(semantic, mention) || strings.Contains(semantic, claim) {
		t.Fatalf("semantic branch must not read the indexed kinds:\n%s", semantic)
	}
	for name, branch := range map[string]string{"indexed": indexed, "semantic": semantic} {
		for _, want := range []string{
			"fact_records.is_tombstone = FALSE",
			"fact_records.payload @>",
			"ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC",
			"LIMIT $",
		} {
			if !strings.Contains(branch, want) {
				t.Fatalf("%s branch missing %q:\n%s", name, want, branch)
			}
		}
	}
	if !strings.Contains(query, "ORDER BY target_facts.observed_at DESC, target_facts.fact_id DESC") {
		t.Fatalf("outer read must re-apply the ordering:\n%s", query)
	}
	// One shared limit parameter, last argument, limit+1 for the truncation sentinel.
	if got, want := args[len(args)-1], 6; got != want {
		t.Fatalf("last arg = %v, want limit+1 = %d", got, want)
	}
	limitParam := fmt.Sprintf("LIMIT $%d", len(args))
	if got := strings.Count(query, limitParam); got != 3 {
		t.Fatalf("%q appears %d times, want 3 (two branches plus outer):\n%s", limitParam, got, query)
	}
}

// TestDocumentationSemanticTargetRefsIndexMatchesQuery binds the semantic
// branch to migration 125. The branch's kind clause is derived from the
// production constant, so a change to the semantic kind, or to the GIN
// opclass the target-ref containment predicate needs, fails here instead of
// silently returning the branch to a heap scan.
func TestDocumentationSemanticTargetRefsIndexMatchesQuery(t *testing.T) {
	t.Parallel()

	migration := normalizeSQLWhitespace(migrationSQLByName(t, "fact_records_documentation_semantic_target_refs_idx"))
	kind := strings.TrimPrefix(documentationTargetSemanticKindClause, "fact_records.")
	for name, fragment := range map[string]string{
		"semantic kind":   kind,
		"tombstone":       "is_tombstone = FALSE",
		"GIN opclass":     "USING GIN (payload jsonb_path_ops)",
		"partial (WHERE)": "WHERE " + kind,
	} {
		if !strings.Contains(migration, fragment) {
			t.Fatalf("migration 125 missing the query's %s %q:\n%s", name, fragment, migration)
		}
	}
	query, _ := buildDocumentationTargetFactsSQL(documentationFindingFilter{Repository: "repo:payments", Limit: 5})
	if !strings.Contains(query, documentationTargetSemanticKindClause) || !strings.Contains(query, "fact_records.payload @>") {
		t.Fatalf("semantic branch no longer reads its kind with payload containment:\n%s", query)
	}
}
