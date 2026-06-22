package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsAreOrderedAndComplete(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) != 49 {
		t.Fatalf("BootstrapDefinitions() len = %d, want 49", len(defs))
	}

	wantNames := []string{
		"ingestion_scopes",
		"scope_generations",
		"fact_records",
		"service_catalog_fact_record_indexes",
		"fact_record_sbom_attestation_indexes",
		"eshu_search_index",
		"eshu_search_vector_metadata",
		"eshu_search_vector_values",
		"content_store",
		"fact_work_items",
		"fact_work_item_audit",
		"semantic_extraction_jobs",
		"collector_generation_dead_letters",
		"governance_audit_events",
		"tenant_workspace_grants",
		"generation_retention_events",
		"scoped_api_tokens",
		"identity_subjects",
		"projection_decisions",
		"admission_decisions",
		"shared_projection_intents",
		"runtime_ingester_control",
		"relationship_tables",
		"shared_projection_acceptance",
		"graph_projection_phase_state",
		"graph_projection_phase_repair_queue",
		"workflow_control_plane",
		"workflow_coordinator_state",
		"deferred_maintenance_barriers",
		"iac_reachability",
		"webhook_refresh_triggers",
		"aws_pagination_checkpoints",
		"aws_scan_status",
		"aws_freshness_triggers",
		"graph_schema_applications",
		"vulnerability_source_states",
		"incident_freshness_triggers",
		"graph_endpoint_presence",
		"service_materialization_generations",
		"service_evidence_snapshots",
		"code_reachability",
		"function_summaries",
		"function_sources",
		"function_graph_ids",
		"admin_replay_requests",
		"value_flow_fixpoint_components",
		"supply_chain_impact_canonical_winners",
		"supply_chain_impact_winners_materialization",
	}
	for i, want := range wantNames {
		if defs[i].Name != want {
			t.Fatalf("BootstrapDefinitions()[%d].Name = %q, want %q", i, defs[i].Name, want)
		}
	}

	for _, def := range defs {
		if strings.TrimSpace(def.Path) == "" {
			t.Fatalf("definition %q has empty path", def.Name)
		}
		if strings.TrimSpace(def.SQL) == "" {
			t.Fatalf("definition %q has empty SQL", def.Name)
		}
	}
}

func TestBootstrapDefinitionsIncludeGraphSchemaApplications(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "graph_schema_applications" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("graph_schema_applications definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS graph_schema_applications",
		"backend TEXT NOT NULL",
		"schema_fingerprint TEXT NOT NULL",
		"compatible_fingerprints JSONB NOT NULL DEFAULT '[]'::jsonb",
		"ADD COLUMN IF NOT EXISTS compatible_fingerprints",
		"PRIMARY KEY (backend, schema_fingerprint)",
		"graph_schema_applications_backend_idx",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("graph_schema_applications SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeScopeGenerationActivityIndex(t *testing.T) {
	t.Parallel()

	var generations Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "scope_generations" {
			generations = def
			break
		}
	}
	if generations.Name == "" {
		t.Fatal("scope_generations definition missing")
	}
	for _, want := range []string{
		"scope_generations_active_pending_activity_idx",
		"GREATEST(observed_at, ingested_at, COALESCE(activated_at, observed_at)) DESC",
		"WHERE status IN ('pending', 'active')",
	} {
		if !strings.Contains(generations.SQL, want) {
			t.Fatalf("scope_generations SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeEshuSearchIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "eshu_search_index" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("eshu_search_index definition missing")
	}
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS eshu_search_index_documents",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_terms",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_stats",
		"REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE",
		"REFERENCES scope_generations(generation_id) ON DELETE CASCADE",
		"CREATE INDEX IF NOT EXISTS eshu_search_index_documents_repo_idx",
		"CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeContentStoreTables(t *testing.T) {
	t.Parallel()

	var contentStore Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "content_store" {
			contentStore = def
			break
		}
	}
	if contentStore.Name == "" {
		t.Fatal("content_store definition missing")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS content_files") {
		t.Fatal("content_store SQL missing content_files table")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS content_entities") {
		t.Fatal("content_store SQL missing content_entities table")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS content_file_references") {
		t.Fatal("content_store SQL missing content_file_references table")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS repository_refs") {
		t.Fatal("content_store SQL missing repository_refs table")
	}
	if !strings.Contains(contentStore.SQL, "repository_refs_repo_default_idx") {
		t.Fatal("content_store SQL missing repository ref default lookup index")
	}
	if !strings.Contains(contentStore.SQL, "content_file_references_lookup_idx") {
		t.Fatal("content_store SQL missing content file reference lookup index")
	}
	if !strings.Contains(contentStore.SQL, "metadata JSONB NOT NULL DEFAULT '{}'::jsonb") {
		t.Fatal("content_store SQL missing content_entities metadata jsonb column")
	}
	if !strings.Contains(contentStore.SQL, "content_files_content_trgm_idx") {
		t.Fatal("content_store SQL missing content_files trigram index")
	}
	if !strings.Contains(contentStore.SQL, "content_entities_source_trgm_idx") {
		t.Fatal("content_store SQL missing content_entities trigram index")
	}
	if !strings.Contains(contentStore.SQL, "content_files_language_repo_idx") {
		t.Fatal("content_store SQL missing language/repository inventory index")
	}
	if !strings.Contains(contentStore.SQL, "ON content_files (language, repo_id)") {
		t.Fatal("content_store SQL missing language/repository index columns")
	}
}

func TestBootstrapDefinitionsIncludeFactContractColumns(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"schema_version TEXT NOT NULL DEFAULT '0.0.0'",
		"collector_kind TEXT NOT NULL DEFAULT 'unknown'",
		"fencing_token BIGINT NOT NULL DEFAULT 0",
		"source_confidence TEXT NOT NULL DEFAULT 'unknown'",
		"ADD COLUMN IF NOT EXISTS schema_version TEXT NOT NULL DEFAULT '0.0.0'",
		"ADD COLUMN IF NOT EXISTS collector_kind TEXT NOT NULL DEFAULT 'unknown'",
		"ADD COLUMN IF NOT EXISTS fencing_token BIGINT NOT NULL DEFAULT 0",
		"ADD COLUMN IF NOT EXISTS source_confidence TEXT NOT NULL DEFAULT 'unknown'",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsWithoutContentSearchIndexesKeepsLookupIndexes(t *testing.T) {
	t.Parallel()

	var contentStore Definition
	for _, def := range BootstrapDefinitionsWithoutContentSearchIndexes() {
		if def.Name == "content_store" {
			contentStore = def
			break
		}
	}
	if contentStore.Name == "" {
		t.Fatal("content_store definition missing")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS content_entities") {
		t.Fatal("content_store SQL missing content_entities table")
	}
	if !strings.Contains(contentStore.SQL, "CREATE TABLE IF NOT EXISTS repository_refs") {
		t.Fatal("content_store SQL missing repository_refs table")
	}
	if !strings.Contains(contentStore.SQL, "content_entities_repo_idx") {
		t.Fatal("content_store SQL missing content entity lookup index")
	}
	if !strings.Contains(contentStore.SQL, "content_file_references_lookup_idx") {
		t.Fatal("content_store SQL missing content file reference lookup index")
	}
	if strings.Contains(contentStore.SQL, "content_files_content_trgm_idx") {
		t.Fatal("content_store SQL includes content_files trigram index")
	}
	if strings.Contains(contentStore.SQL, "content_entities_source_trgm_idx") {
		t.Fatal("content_store SQL includes content_entities trigram index")
	}
}

func TestEnsureContentSearchIndexesAppliesOnlyTrigramIndexes(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	if err := EnsureContentSearchIndexes(context.Background(), exec); err != nil {
		t.Fatalf("EnsureContentSearchIndexes() error = %v, want nil", err)
	}
	if len(exec.statements) != 1 {
		t.Fatalf("EnsureContentSearchIndexes() statements = %d, want 1", len(exec.statements))
	}
	statement := exec.statements[0]
	if !strings.Contains(statement, "content_files_content_trgm_idx") {
		t.Fatal("content search index SQL missing file trigram index")
	}
	if !strings.Contains(statement, "content_entities_source_trgm_idx") {
		t.Fatal("content search index SQL missing entity trigram index")
	}
	if strings.Contains(statement, "CREATE TABLE") {
		t.Fatal("content search index SQL unexpectedly creates tables")
	}
}
