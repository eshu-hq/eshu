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

	// Projection state seeder: uses exact count from fact_records.
	for _, want := range []string{
		"INSERT INTO eshu_search_document_projection_state",
		"SELECT count(*)",
		"FROM fact_records",
		"WHERE s.scope_kind = 'repository'",
		"f.fact_kind = $1",
		"f.is_tombstone = FALSE",
		"NOT EXISTS",
		"eshu_search_document_projection_state",
	} {
		if !strings.Contains(seedProjectionStateSQL, want) {
			t.Fatalf("seedProjectionStateSQL missing %q", want)
		}
	}

	// Vector scope state seeder: uses exact anti-join matching the OLD pending query.
	for _, want := range []string{
		"INSERT INTO eshu_search_vector_scope_state",
		"WHERE ps.document_count > 0",
		"NOT EXISTS",
		"eshu_search_vector_metadata",
		"LEFT JOIN eshu_search_vector_values value",
		"meta.embedding_content_hash = fact.payload->>'content_hash'",
		"meta.build_state = 'disabled'",
		"meta.build_state = 'ready'",
		"value.document_id IS NOT NULL",
		"eshu_search_vector_scope_state vs",
	} {
		if !strings.Contains(seedVectorScopeStateSQL, want) {
			t.Fatalf("seedVectorScopeStateSQL missing %q", want)
		}
	}
}
