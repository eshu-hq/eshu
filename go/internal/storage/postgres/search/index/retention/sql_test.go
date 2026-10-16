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
