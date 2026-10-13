// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// The shipped entity SQL carries the exactness invariants the fake cannot
// assume: a strict four-part keyset (the cursor names one entity, and three
// entities share a file, so a file-key resume would return rows twice), an
// edge row at the window's last position, the walk-order index order, and the
// full row payload (an exact entity page must equal the old statement's page,
// which carried source_cache and metadata). These tests pin the text so a
// changed operator, a dropped key part, or a lost column fails before any
// database runs.
func TestEntityStatementsCarryStrictFourPartKeysetAndFullPayload(t *testing.T) {
	t.Parallel()

	statements := unscoped.EntityStatements()
	const keyOrder = "ORDER BY repo_id, relative_path, start_line, entity_id"
	const stepKeyset = "(repo_id, relative_path, start_line, entity_id) > ($4::text, $5::text, $6::integer, $7::text)"
	if got := strings.Count(statements.StepAfter, stepKeyset); got != 2 {
		t.Errorf("StepAfter has %d four-part keyset predicates, want 2 (window and edge branch)", got)
	}
	const tailKeyset = "(repo_id, relative_path, start_line, entity_id) > ($3::text, $4::text, $5::integer, $6::text)"
	if got := strings.Count(statements.TailAfter, tailKeyset); got != 1 {
		t.Errorf("TailAfter has %d four-part keyset predicates, want 1", got)
	}
	for name, sql := range map[string]string{
		"StepFirst": statements.StepFirst, "StepAfter": statements.StepAfter,
		"TailFirst": statements.TailFirst, "TailAfter": statements.TailAfter,
	} {
		if strings.Contains(sql, ">=") || strings.Contains(sql, "<") {
			t.Errorf("%s uses an inclusive or reversed comparison: %s", name, sql)
		}
		if !strings.Contains(sql, keyOrder) {
			t.Errorf("%s lost its key order: %s", name, sql)
		}
		if !strings.Contains(sql, "source_cache ILIKE '%' || $1 || '%'") {
			t.Errorf("%s lost its substring predicate: %s", name, sql)
		}
		if !strings.Contains(sql, "FROM content_entities") {
			t.Errorf("%s reads the wrong table: %s", name, sql)
		}
	}
	for name, sql := range map[string]string{"StepFirst": statements.StepFirst, "StepAfter": statements.StepAfter} {
		if got := strings.Count(sql, "OFFSET ($2::bigint - 1) LIMIT 1"); got != 1 {
			t.Errorf("%s edge branch must be OFFSET ($2::bigint - 1) LIMIT 1, found %d", name, got)
		}
		// The window visits whole rows: the outer hit projection must carry
		// the payload the old statement returned, or an exact page differs.
		for _, column := range []string{"entity_id", "entity_type", "entity_name", "start_line", "end_line", "source_cache", "metadata"} {
			if !strings.Contains(sql, column) {
				t.Errorf("%s lost payload column %s", name, column)
			}
		}
	}
	for name, sql := range map[string]string{"StepFirst": statements.StepFirst, "TailFirst": statements.TailFirst} {
		if strings.Contains(sql, "(repo_id, relative_path, start_line, entity_id) >") {
			t.Errorf("%s must not carry a keyset predicate", name)
		}
	}
	for name, sql := range map[string]string{"TailFirst": statements.TailFirst, "TailAfter": statements.TailAfter} {
		if !strings.HasSuffix(strings.TrimSpace(sql), "LIMIT $2::bigint") {
			t.Errorf("%s must end with LIMIT $2::bigint (the live oracle derives from it): %s", name, sql)
		}
	}
}
