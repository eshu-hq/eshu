// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Definition describes one ordered bootstrap SQL payload.
type Definition struct {
	Name string
	Path string
	SQL  string
}

// Executor is the narrow adapter surface required to apply schema bootstrap
// statements against a SQL connection or transaction.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var bootstrapDefinitions = []Definition{
	{
		Name: "ingestion_scopes",
		Path: "schema/data-plane/postgres/001_ingestion_scopes.sql",
		SQL:  scopeSchemaSQL,
	},
	{
		Name: "scope_generations",
		Path: "schema/data-plane/postgres/002_scope_generations.sql",
		SQL:  scopeGenerationSchemaSQL,
	},
	{
		Name: "fact_records",
		Path: "schema/data-plane/postgres/003_fact_records.sql",
		SQL:  factRecordSchemaSQL,
	},
	{
		Name: "eshu_search_index",
		Path: "schema/data-plane/postgres/003b_eshu_search_index.sql",
		SQL:  eshuSearchIndexSchemaSQL,
	},
	{
		Name: "eshu_search_vector_metadata",
		Path: "schema/data-plane/postgres/003c_eshu_search_vector_metadata.sql",
		SQL:  eshuSearchVectorMetadataSchemaSQL,
	},
	{
		Name: "eshu_search_vector_values",
		Path: "schema/data-plane/postgres/003d_eshu_search_vector_values.sql",
		SQL:  eshuSearchVectorValuesSchemaSQL,
	},
	{
		Name: "service_catalog_fact_record_indexes",
		Path: "schema/data-plane/postgres/003_service_catalog_fact_record_indexes.sql",
		SQL:  serviceCatalogFactRecordReadIndexesSQL,
	},
	{
		Name: "fact_record_sbom_attestation_indexes",
		Path: "schema/data-plane/postgres/003a_fact_record_sbom_attestation_indexes.sql",
		SQL:  factRecordSBOMAttestationReadIndexesSQL,
	},
	{
		Name: "content_store",
		Path: "schema/data-plane/postgres/004_content_store.sql",
		SQL:  contentStoreSchemaSQL,
	},
	{
		Name: "fact_work_items",
		Path: "schema/data-plane/postgres/005_fact_work_items.sql",
		SQL:  workItemSchemaSQL,
	},
	{
		Name: "fact_work_item_audit",
		Path: "schema/data-plane/postgres/006_fact_work_item_audit.sql",
		SQL:  workItemAuditSchemaSQL,
	},
	{
		Name: "tenant_workspace_grants",
		Path: "schema/data-plane/postgres/006c_tenant_workspace_grants.sql",
		SQL:  tenantWorkspaceGrantSchemaSQL,
	},
	{
		Name: "graph_projection_phase_repair_queue",
		Path: "schema/data-plane/postgres/013_graph_projection_phase_repair_queue.sql",
		SQL:  graphProjectionPhaseRepairQueueSchemaSQL,
	},
	{
		Name: "function_summaries",
		Path: "schema/data-plane/postgres/028_function_summaries.sql",
		SQL:  functionSummarySchemaSQL,
	},
}

const scopeSchemaSQL = `
CREATE TABLE IF NOT EXISTS ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS ingestion_scopes_source_idx
    ON ingestion_scopes (
        source_system,
        scope_kind,
        partition_key,
        observed_at DESC
    );

CREATE INDEX IF NOT EXISTS ingestion_scopes_parent_idx
    ON ingestion_scopes (parent_scope_id, observed_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS ingestion_scopes_active_generation_idx
    ON ingestion_scopes (active_generation_id)
    WHERE active_generation_id IS NOT NULL;
`

const scopeGenerationSchemaSQL = `
CREATE TABLE IF NOT EXISTS scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Additive migration for installs created before the delta-correctness
-- baseline (epic #2340): source_commit_sha carries the commit a generation was
-- observed from so the next git sync can baseline its delta on the last
-- successfully projected commit instead of the local working-copy HEAD; is_delta
-- marks delta resyncs so the reconciliation sweep can find the last full
-- observation per scope.
ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS source_commit_sha TEXT NULL;

ALTER TABLE scope_generations
    ADD COLUMN IF NOT EXISTS is_delta BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);

-- Backs the latest-generation DISTINCT ON (issue #3704): the relationship
-- backfill and active-generation lookups pick each scope's newest generation by
-- ORDER BY (scope_id, ingested_at DESC, generation_id DESC). The status-leading
-- scope_generations_scope_idx cannot serve that ordering without a sort because
-- status sits between scope_id and ingested_at; this index leads straight into
-- the DISTINCT ON sort key so the per-scope newest row is an index read.
CREATE INDEX IF NOT EXISTS scope_generations_scope_latest_lookup_idx
    ON scope_generations (scope_id, ingested_at DESC, generation_id DESC);

CREATE INDEX IF NOT EXISTS scope_generations_active_pending_activity_idx
    ON scope_generations (GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC)
    WHERE status IN ('pending', 'active');

CREATE UNIQUE INDEX IF NOT EXISTS scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';
`

const eshuSearchIndexSchemaSQL = `
CREATE TABLE IF NOT EXISTS eshu_search_index_documents (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    fact_id TEXT NOT NULL,
    repo_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    document JSONB NOT NULL,
    document_length INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id, document_id)
);

CREATE TABLE IF NOT EXISTS eshu_search_index_terms (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_id TEXT NOT NULL,
    term_key TEXT NOT NULL,
    term TEXT NOT NULL,
    term_frequency INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS eshu_search_index_stats (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    document_count INTEGER NOT NULL,
    average_document_length DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, generation_id)
);

ALTER TABLE eshu_search_index_terms
    ADD COLUMN IF NOT EXISTS term_key TEXT;

WITH stale_index_generations AS (
    SELECT DISTINCT scope_id, generation_id
    FROM eshu_search_index_terms
    WHERE term_key IS NULL OR term_key = ''
)
DELETE FROM eshu_search_index_stats stats
USING stale_index_generations stale
WHERE stats.scope_id = stale.scope_id
  AND stats.generation_id = stale.generation_id;

DELETE FROM eshu_search_index_terms
WHERE term_key IS NULL OR term_key = '';

DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx;

ALTER TABLE eshu_search_index_terms
    ALTER COLUMN term_key SET NOT NULL;

ALTER TABLE eshu_search_index_terms
    DROP CONSTRAINT IF EXISTS eshu_search_index_terms_pkey;

ALTER TABLE eshu_search_index_terms
    ADD CONSTRAINT eshu_search_index_terms_pkey
    PRIMARY KEY (scope_id, generation_id, term_key, document_id);

CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx
    ON eshu_search_index_documents (scope_id, generation_id, repo_id, source_kind);

CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx
    ON eshu_search_index_terms (scope_id, generation_id, term_key);
`

const workItemSchemaSQL = `
CREATE TABLE IF NOT EXISTS fact_work_items (
    work_item_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage TEXT NOT NULL,
    domain TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key TEXT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NULL,
    claim_until TIMESTAMPTZ NULL,
    visible_at TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_domain TEXT NOT NULL DEFAULT 'scope';

ALTER TABLE fact_work_items
    ADD COLUMN IF NOT EXISTS conflict_key TEXT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_scope_generation_idx
    ON fact_work_items (scope_id, generation_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_status_idx
    ON fact_work_items (status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_stage_domain_status_idx
    ON fact_work_items (stage, domain, status, visible_at, updated_at DESC);

CREATE INDEX IF NOT EXISTS fact_work_items_claim_until_idx
    ON fact_work_items (claim_until)
    WHERE claim_until IS NOT NULL;

CREATE INDEX IF NOT EXISTS fact_work_items_reducer_conflict_claim_idx
    ON fact_work_items (stage, conflict_domain, COALESCE(conflict_key, scope_id), status, claim_until, updated_at DESC)
    WHERE stage = 'reducer';
`

const workItemAuditSchemaSQL = `
CREATE TABLE IF NOT EXISTS fact_replay_events (
    replay_event_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL REFERENCES fact_work_items(work_item_id) ON DELETE CASCADE,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    failure_class TEXT NULL,
    operator_note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS fact_replay_events_work_item_idx
    ON fact_replay_events (work_item_id, created_at DESC);

CREATE TABLE IF NOT EXISTS fact_backfill_requests (
    backfill_request_id TEXT PRIMARY KEY,
    scope_id TEXT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE SET NULL,
    generation_id TEXT NULL REFERENCES scope_generations(generation_id) ON DELETE SET NULL,
    operator_note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS fact_backfill_requests_scope_idx
    ON fact_backfill_requests (scope_id, generation_id, created_at DESC);
`

// BootstrapDefinitions returns a copy of the ordered Wave 2 bootstrap layout.
func BootstrapDefinitions() []Definition {
	defs := append([]Definition(nil), bootstrapDefinitions...)
	sort.SliceStable(defs, func(i, j int) bool {
		return defs[i].Path < defs[j].Path
	})
	return defs
}

// BootstrapDefinitionsWithoutContentSearchIndexes returns the bootstrap layout
// without the expensive content trigram indexes. It is intended for
// local-authoritative bulk-load flows that call EnsureContentSearchIndexes
// after the initial write-heavy drain completes.
func BootstrapDefinitionsWithoutContentSearchIndexes() []Definition {
	defs := BootstrapDefinitions()
	for i := range defs {
		if defs[i].Name == "content_store" {
			defs[i].SQL = contentStoreSchemaWithoutSearchIndexesSQL
			break
		}
	}
	return defs
}

// BootstrapStatements returns the ordered SQL payloads that make up the
// bootstrap layout.
func BootstrapStatements() []string {
	defs := BootstrapDefinitions()
	statements := make([]string, 0, len(defs))
	for _, def := range defs {
		statements = append(statements, def.SQL)
	}

	return statements
}

// ValidateDefinitions checks that a schema layout is complete enough to apply.
func ValidateDefinitions(defs []Definition) error {
	seen := make(map[string]struct{}, len(defs))
	for i, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			return fmt.Errorf("definition %d has an empty name", i)
		}
		if strings.TrimSpace(def.Path) == "" {
			return fmt.Errorf("definition %q has an empty path", def.Name)
		}
		if strings.TrimSpace(def.SQL) == "" {
			return fmt.Errorf("definition %q has empty SQL", def.Name)
		}
		if _, ok := seen[def.Name]; ok {
			return fmt.Errorf("duplicate definition name %q", def.Name)
		}
		seen[def.Name] = struct{}{}
	}

	return nil
}

// ApplyDefinitions executes one ordered schema layout against the executor.
func ApplyDefinitions(ctx context.Context, exec Executor, defs []Definition) error {
	if err := ValidateDefinitions(defs); err != nil {
		return err
	}
	if exec == nil {
		return fmt.Errorf("executor is required")
	}

	for _, def := range defs {
		if _, err := exec.ExecContext(ctx, def.SQL); err != nil {
			return fmt.Errorf("apply %s: %w", def.Name, err)
		}
	}

	return nil
}

// ApplyBootstrap applies the Wave 2 schema bootstrap layout.
func ApplyBootstrap(ctx context.Context, exec Executor) error {
	return ApplyDefinitions(ctx, exec, BootstrapDefinitions())
}

// ApplyBootstrapWithoutContentSearchIndexes applies the bootstrap layout while
// deferring content trigram indexes for a later bulk index build.
func ApplyBootstrapWithoutContentSearchIndexes(ctx context.Context, exec Executor) error {
	return ApplyDefinitions(ctx, exec, BootstrapDefinitionsWithoutContentSearchIndexes())
}

// EnsureContentSearchIndexes creates the trigram indexes that accelerate
// content file and entity source search.
func EnsureContentSearchIndexes(ctx context.Context, exec Executor) error {
	if exec == nil {
		return fmt.Errorf("executor is required")
	}
	if _, err := exec.ExecContext(ctx, contentStoreSearchIndexSchemaSQL); err != nil {
		return fmt.Errorf("ensure content search indexes: %w", err)
	}
	return nil
}
