// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// SeedSearchVectorScopeState: validation
// ---------------------------------------------------------------------------

func TestSeedSearchVectorScopeStateRequiresDatabase(t *testing.T) {
	t.Parallel()

	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "local",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "m1",
		VectorIndexVersion: "v1",
	}
	err := SeedSearchVectorScopeState(context.Background(), nil, identity)
	if err == nil {
		t.Fatal("expected error when database is nil")
	}
}

func TestSeedSearchVectorScopeStateRequiresIdentityFields(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	err := SeedSearchVectorScopeState(context.Background(), db, EshuSearchVectorIdentity{})
	if err == nil {
		t.Fatal("expected error when identity fields are empty")
	}
	if !strings.Contains(err.Error(), "provider profile id") {
		t.Fatalf("error missing provider profile: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Seeder SQL shape guards
// ---------------------------------------------------------------------------

func TestSeedSearchVectorScopeStateSQLShapes(t *testing.T) {
	t.Parallel()

	// Projection state seeder counts the persisted search-index projection, the
	// same source consumed by the vector builder.
	for _, want := range []string{
		"INSERT INTO eshu_search_document_projection_state",
		"SELECT count(*)",
		"FROM eshu_search_index_documents",
		"WHERE s.scope_kind = 'repository'",
		"NOT EXISTS",
		"eshu_search_document_projection_state",
	} {
		if !strings.Contains(seedProjectionStateSQL, want) {
			t.Fatalf("seedProjectionStateSQL missing %q", want)
		}
	}
	if strings.Contains(seedProjectionStateSQL, "fact_records") {
		t.Fatalf("seedProjectionStateSQL must not rescan fact_records JSON:\n%s", seedProjectionStateSQL)
	}

	// Vector scope state seeder records conservative building rows only. Exact
	// completeness belongs to the bounded scheduler, not synchronous startup.
	for _, want := range []string{
		"INSERT INTO eshu_search_vector_scope_state",
		"JOIN ingestion_scopes scope",
		"ps.state = 'ready'",
		"ps.document_count > 0",
		"'building'",
		"NOT EXISTS",
		"eshu_search_vector_scope_state vs",
	} {
		if !strings.Contains(seedVectorScopeStateSQL, want) {
			t.Fatalf("seedVectorScopeStateSQL missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"fact_records",
		"eshu_search_index_documents",
		"eshu_search_vector_metadata",
		"eshu_search_vector_values",
	} {
		if strings.Contains(seedVectorScopeStateSQL, forbidden) {
			t.Fatalf("seedVectorScopeStateSQL contains synchronous exact-ready shape %q:\n%s", forbidden, seedVectorScopeStateSQL)
		}
	}
}
