// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package indexretention

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestLockStatementsUseLockClass pins the class literal in every advisory
// lock statement to LockClass: the module-wide collision scan reads the
// literal, so the constant and the SQL must agree.
func TestLockStatementsUseLockClass(t *testing.T) {
	want := fmt.Sprintf("pg_try_advisory_xact_lock(%d, hashtext(", LockClass)
	for name, query := range map[string]string{"batch": tryLockQuery, "prune": tryLockGenerationsQuery} {
		if !strings.Contains(query, want) {
			t.Fatalf("%s lock statement %q does not take %s", name, query, want)
		}
	}
}

// TestBatchStatementsStayOnOneGenerationAndLeaf pins the predicates that make
// a ctid match safe: the page and the DELETE each filter on scope_id = $1
// (one hash leaf) and generation_id = $2, and the DELETE matches the page's
// ctids. It also pins the in-statement eligibility re-check (#7427 c4/c5).
func TestBatchStatementsStayOnOneGenerationAndLeaf(t *testing.T) {
	for name, query := range map[string]string{
		"terms":     deleteTermsBatchQuery,
		"documents": deleteDocumentsBatchQuery,
	} {
		for _, fragment := range []string{
			"generation.status = 'superseded'",
			"generation.superseded_at IS NOT NULL",
			"scope.active_generation_id IS DISTINCT FROM $2",
			"work.status IN ('claimed', 'running', 'retrying')",
			"AND EXISTS (SELECT 1 FROM eligible)",
			".ctid = ANY(ARRAY(SELECT page.row_ctid FROM page))",
		} {
			if !strings.Contains(query, fragment) {
				t.Fatalf("%s batch lacks %q", name, fragment)
			}
		}
		page := query[strings.Index(query, "page AS MATERIALIZED"):strings.Index(query, "deleted AS (")]
		deleted := query[strings.Index(query, "deleted AS ("):strings.Index(query, "last_key AS (")]
		for part, text := range map[string]string{"page": page, "delete": deleted} {
			if !regexp.MustCompile(`\.scope_id = \$1\b`).MatchString(text) ||
				!regexp.MustCompile(`\.generation_id = \$2\b`).MatchString(text) {
				t.Fatalf("%s batch %s does not filter on scope_id = $1 and generation_id = $2:\n%s", name, part, text)
			}
		}
	}
	if !strings.Contains(deleteTermsBatchQuery, "ORDER BY term.term_key, term.document_id") {
		t.Fatal("terms page must walk the primary key order")
	}
	if !strings.Contains(deleteDocumentsBatchQuery, "ORDER BY document.document_id") {
		t.Fatal("documents page must walk the primary key order")
	}
}

// TestOrphanSweepStatementsDeleteOnlyMissingGenerations pins the orphan
// sweep's eligibility (#7427 PR2): a batch deletes only when the (scope_id,
// generation_id) pair is absent from scope_generations and the scope's active
// pointer does not name the generation, and it shares the pre-delete's page
// and ctid DELETE so it stays on one generation and one hash leaf.
func TestOrphanSweepStatementsDeleteOnlyMissingGenerations(t *testing.T) {
	for name, pair := range map[string][2]string{
		"terms":     {sweepTermsBatchQuery, deleteTermsBatchQuery},
		"documents": {sweepDocumentsBatchQuery, deleteDocumentsBatchQuery},
	} {
		sweep, predelete := pair[0], pair[1]
		for _, fragment := range []string{
			"WHERE NOT EXISTS (",
			"FROM scope_generations AS generation",
			"generation.generation_id = $2",
			"generation.scope_id = $1",
			"scope.active_generation_id = $2",
			"AND EXISTS (SELECT 1 FROM eligible)",
		} {
			if !strings.Contains(sweep, fragment) {
				t.Fatalf("%s orphan sweep batch lacks %q:\n%s", name, fragment, sweep)
			}
		}
		if strings.Contains(sweep, "'superseded'") {
			t.Fatalf("%s orphan sweep batch must not admit an existing superseded generation", name)
		}
		start := strings.Index(predelete, "page AS MATERIALIZED")
		if start < 0 {
			t.Fatalf("%s pre-delete batch lacks its page CTE", name)
		}
		body := predelete[start:]
		if !strings.HasSuffix(sweep, body) {
			t.Fatalf("%s orphan sweep batch does not share the pre-delete page and DELETE", name)
		}
	}
}

// TestCensusStatementWalksThePrimaryKeyPrefix pins the census to the loose
// index scan over (scope_id, generation_id): a recursive step that seeks past
// the previous pair, never a DISTINCT over the table (#7427 PR2 evidence: the
// DISTINCT form read the whole 10M-row leaf).
func TestCensusStatementWalksThePrimaryKeyPrefix(t *testing.T) {
	query := censusQuery(`"eshu_search_index_terms_p00"`)
	for _, fragment := range []string{
		"WITH RECURSIVE pairs AS (",
		`FROM "eshu_search_index_terms_p00" AS relation`,
		"ORDER BY relation.scope_id, relation.generation_id",
		"(relation.scope_id, relation.generation_id) > (pairs.scope_id, pairs.generation_id)",
		"FROM scope_generations AS generation",
		"generation.generation_id = pairs.generation_id",
		"generation.scope_id = pairs.scope_id",
		"LIMIT $1",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("census lacks %q:\n%s", fragment, query)
		}
	}
	if strings.Contains(strings.ToUpper(query), "DISTINCT") {
		t.Fatalf("census must not use DISTINCT:\n%s", query)
	}
}
