// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Schema SQL parity tests
// ---------------------------------------------------------------------------

func TestEshuSearchDocumentProjectionStateSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := EshuSearchDocumentProjectionStateSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS eshu_search_document_projection_state",
		"scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"projection_revision BIGINT NOT NULL",
		"build_fence BIGINT NOT NULL",
		"state TEXT NOT NULL CHECK (state IN ('building','ready','failed'))",
		"document_count BIGINT NOT NULL DEFAULT 0",
		"updated_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (scope_id, generation_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

func TestEshuSearchVectorScopeStateSchemaSQL(t *testing.T) {
	t.Parallel()

	sql := EshuSearchVectorScopeStateSchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS eshu_search_vector_scope_state",
		"scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"provider_profile_id TEXT NOT NULL",
		"source_class TEXT NOT NULL",
		"embedding_model_id TEXT NOT NULL",
		"vector_index_version TEXT NOT NULL",
		"projection_revision BIGINT NOT NULL",
		"build_fence BIGINT NOT NULL",
		"state TEXT NOT NULL CHECK (state IN ('building','ready','failed'))",
		"updated_at TIMESTAMPTZ NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, provider_profile_id, source_class, embedding_model_id, vector_index_version)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("schema missing %q:\n%s", want, sql)
		}
	}
}

// ---------------------------------------------------------------------------
// BootstrapDefinitions inclusion tests
// ---------------------------------------------------------------------------

func TestBootstrapDefinitionsIncludeEshuSearchDocumentProjectionState(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_document_projection_state" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing eshu_search_document_projection_state")
	}
	if found.Path != "go/internal/storage/postgres/migrations/054_eshu_search_document_projection_state.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS eshu_search_document_projection_state") {
		t.Fatalf("definition SQL missing table:\n%s", found.SQL)
	}
}

func TestBootstrapDefinitionsIncludeEshuSearchVectorScopeState(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_vector_scope_state" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("BootstrapDefinitions missing eshu_search_vector_scope_state")
	}
	if found.Path != "go/internal/storage/postgres/migrations/055_eshu_search_vector_scope_state.sql" {
		t.Fatalf("Path = %q", found.Path)
	}
	if !strings.Contains(found.SQL, "CREATE TABLE IF NOT EXISTS eshu_search_vector_scope_state") {
		t.Fatalf("definition SQL missing table:\n%s", found.SQL)
	}
}
