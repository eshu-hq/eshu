// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

func TestWorkItemEvidenceQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"FROM fact_records AS fact",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = fact.generation_id",
		"JOIN scope_generations AS generation",
		"generation.status = 'active'",
		"fact.is_tombstone = FALSE",
		"fact.fact_kind = ANY($1::text[])",
		"fact.scope_id = $2",
		"fact.payload->>'work_item_key' = $3",
		"fact.payload->>'provider_work_item_id' = $4",
		"fact.payload->>'project_key' = $5",
		"fact.payload->>'url_fingerprint' = $6",
		"fact.payload->>'source_url_fingerprint' = $6",
		"fact.observed_at >= $7",
		"fact.fact_id > $8",
		"COALESCE(cardinality($9::text[]), 0) = 0",
		"fact.payload->>'linked_repository_id' = ANY($9::text[])",
		"ORDER BY fact.fact_id ASC",
		"LIMIT $10",
	} {
		if !strings.Contains(listWorkItemEvidenceQuery, want) {
			t.Fatalf("listWorkItemEvidenceQuery missing %q:\n%s", want, listWorkItemEvidenceQuery)
		}
	}
}

func TestWorkItemEvidenceQueryAvoidsRawURLMatching(t *testing.T) {
	t.Parallel()

	for _, forbidden := range []string{
		"payload->>'url' =",
		"payload->>'source_url' =",
		"LOWER(fact.payload",
		"jsonb_array_elements",
	} {
		if strings.Contains(listWorkItemEvidenceQuery, forbidden) {
			t.Fatalf("listWorkItemEvidenceQuery contains forbidden predicate %q:\n%s", forbidden, listWorkItemEvidenceQuery)
		}
	}
}

func TestWorkItemEvidenceQueryTreatsNullGrantArrayAsUnscoped(t *testing.T) {
	t.Parallel()

	bound, err := pgarray.Array([]string(nil)).Value()
	if err != nil {
		t.Fatalf("nil grant array Value() error = %v", err)
	}
	if bound != nil {
		t.Fatalf("nil grant array Value() = %#v, want SQL NULL", bound)
	}
	if !strings.Contains(listWorkItemEvidenceQuery, "COALESCE(cardinality($9::text[]), 0) = 0") {
		t.Fatalf("query does not preserve shared/admin/local visibility for a NULL grant array:\n%s", listWorkItemEvidenceQuery)
	}
}
