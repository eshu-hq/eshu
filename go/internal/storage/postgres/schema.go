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
		Name: "graph_projection_phase_repair_queue",
		Path: "schema/data-plane/postgres/013_graph_projection_phase_repair_queue.sql",
		SQL:  graphProjectionPhaseRepairQueueSchemaSQL,
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
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS scope_generations_scope_idx
    ON scope_generations (scope_id, status, ingested_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS scope_generations_active_scope_idx
    ON scope_generations (scope_id)
    WHERE status = 'active';
`

const factRecordSchemaSQL = `
CREATE TABLE IF NOT EXISTS fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT '0.0.0';

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS collector_kind TEXT NOT NULL DEFAULT 'unknown';

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0;

ALTER TABLE fact_records
    ADD COLUMN IF NOT EXISTS source_confidence TEXT NOT NULL DEFAULT 'unknown';

CREATE INDEX IF NOT EXISTS fact_records_scope_generation_idx
    ON fact_records (scope_id, generation_id, fact_kind, observed_at DESC);

CREATE INDEX IF NOT EXISTS fact_records_stable_key_idx
    ON fact_records (stable_fact_key, generation_id);

CREATE INDEX IF NOT EXISTS fact_records_framework_routes_repo_path_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'relative_path'))
    WHERE fact_kind = 'file'
      AND payload->'parsed_file_data'->'framework_semantics' IS NOT NULL
      AND jsonb_array_length(
          COALESCE(payload->'parsed_file_data'->'framework_semantics'->'frameworks', '[]'::jsonb)
      ) > 0;

CREATE INDEX IF NOT EXISTS fact_records_documentation_findings_visible_idx
    ON fact_records (
        (payload->>'finding_type'),
        (payload->>'source_id'),
        (payload->>'document_id'),
        (payload->>'status'),
        (payload->>'truth_level'),
        (payload->>'freshness_state'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_finding'
      AND is_tombstone = FALSE
      AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
      AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
      AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied';

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_finding_idx
    ON fact_records (
        COALESCE(payload->>'finding_id', payload->'finding'->>'finding_id'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_evidence_packet'
      AND is_tombstone = FALSE;

CREATE INDEX IF NOT EXISTS fact_records_documentation_packets_packet_idx
    ON fact_records (
        (payload->>'packet_id'),
        observed_at DESC,
        fact_id DESC
    )
    WHERE fact_kind = 'documentation_evidence_packet'
      AND is_tombstone = FALSE;
`

const contentStoreBaseSchemaSQL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS content_files (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    commit_sha TEXT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    line_count INTEGER NOT NULL,
    language TEXT NULL,
    indexed_at TIMESTAMPTZ NOT NULL,
    artifact_type TEXT NULL,
    template_dialect TEXT NULL,
    iac_relevant BOOLEAN NULL,
    PRIMARY KEY (repo_id, relative_path)
);

CREATE TABLE IF NOT EXISTS content_entities (
    entity_id TEXT PRIMARY KEY,
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_name TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    start_byte INTEGER NULL,
    end_byte INTEGER NULL,
    language TEXT NULL,
    source_cache TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    indexed_at TIMESTAMPTZ NOT NULL,
    artifact_type TEXT NULL,
    template_dialect TEXT NULL,
    iac_relevant BOOLEAN NULL
);

CREATE TABLE IF NOT EXISTS content_file_references (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    reference_kind TEXT NOT NULL,
    reference_value TEXT NOT NULL,
    indexed_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (repo_id, relative_path, reference_kind, reference_value)
);

CREATE INDEX IF NOT EXISTS content_files_repo_path_idx
    ON content_files (repo_id, relative_path);
CREATE INDEX IF NOT EXISTS content_entities_repo_idx
    ON content_entities (repo_id);
CREATE INDEX IF NOT EXISTS content_entities_type_idx
    ON content_entities (entity_type);
CREATE INDEX IF NOT EXISTS content_entities_path_idx
    ON content_entities (relative_path);
CREATE INDEX IF NOT EXISTS content_file_references_lookup_idx
    ON content_file_references (reference_kind, reference_value, repo_id);
CREATE INDEX IF NOT EXISTS content_file_references_repo_path_idx
    ON content_file_references (repo_id, relative_path);
`

const contentStoreSearchIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_content_trgm_idx
    ON content_files USING gin (content gin_trgm_ops);
CREATE INDEX IF NOT EXISTS content_entities_source_trgm_idx
    ON content_entities USING gin (source_cache gin_trgm_ops);
`

const contentStoreFilterIndexSchemaSQL = `CREATE INDEX IF NOT EXISTS content_files_artifact_type_idx
    ON content_files (artifact_type);
CREATE INDEX IF NOT EXISTS content_files_template_dialect_idx
    ON content_files (template_dialect);
CREATE INDEX IF NOT EXISTS content_files_iac_relevant_idx
    ON content_files (iac_relevant);
CREATE INDEX IF NOT EXISTS content_entities_artifact_type_idx
    ON content_entities (artifact_type);
CREATE INDEX IF NOT EXISTS content_entities_template_dialect_idx
    ON content_entities (template_dialect);
CREATE INDEX IF NOT EXISTS content_entities_iac_relevant_idx
    ON content_entities (iac_relevant);
`

const contentStoreSchemaSQL = contentStoreBaseSchemaSQL + contentStoreSearchIndexSchemaSQL + contentStoreFilterIndexSchemaSQL

const contentStoreSchemaWithoutSearchIndexesSQL = contentStoreBaseSchemaSQL + contentStoreFilterIndexSchemaSQL

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
