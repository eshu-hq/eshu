package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsAreOrderedAndComplete(t *testing.T) {
	t.Parallel()

	defs := BootstrapDefinitions()
	if len(defs) != 20 {
		t.Fatalf("BootstrapDefinitions() len = %d, want 20", len(defs))
	}

	wantNames := []string{
		"ingestion_scopes",
		"scope_generations",
		"fact_records",
		"content_store",
		"fact_work_items",
		"fact_work_item_audit",
		"projection_decisions",
		"shared_projection_intents",
		"runtime_ingester_control",
		"relationship_tables",
		"shared_projection_acceptance",
		"graph_projection_phase_state",
		"graph_projection_phase_repair_queue",
		"workflow_control_plane",
		"workflow_coordinator_state",
		"iac_reachability",
		"webhook_refresh_triggers",
		"aws_pagination_checkpoints",
		"aws_scan_status",
		"aws_freshness_triggers",
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
}

func TestBootstrapDefinitionsIncludeFrameworkRouteFactIndex(t *testing.T) {
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
		"fact_records_framework_routes_repo_path_idx",
		"(payload->>'repo_id')",
		"(payload->>'relative_path')",
		"payload->'parsed_file_data'->'framework_semantics' IS NOT NULL",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeActiveRepositoryFactIndex(t *testing.T) {
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
		"fact_records_active_repository_idx",
		"ON fact_records (observed_at ASC, fact_id ASC, generation_id)",
		"WHERE fact_kind = 'repository'",
		"AND source_system = 'git'",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeDocumentationFactIndexes(t *testing.T) {
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
		"fact_records_documentation_findings_visible_idx",
		"fact_records_documentation_packets_finding_idx",
		"fact_records_documentation_packets_packet_idx",
		"WHERE fact_kind = 'documentation_finding'",
		"WHERE fact_kind = 'documentation_evidence_packet'",
		"(payload->'permissions'->>'viewer_can_read_source') = 'true'",
		"LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'",
		"LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'",
		"payload->>'finding_id'",
		"(payload->>'packet_id')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
	start := strings.Index(facts.SQL, "CREATE INDEX IF NOT EXISTS fact_records_documentation_findings_visible_idx")
	if start < 0 {
		t.Fatal("documentation findings index missing")
	}
	indexSQL := facts.SQL[start:]
	filterKey := strings.Index(indexSQL, "(payload->>'finding_type')")
	orderKey := strings.Index(indexSQL, "observed_at DESC")
	if filterKey < 0 || orderKey < 0 {
		t.Fatalf("documentation findings index missing filter or order keys: %s", indexSQL)
	}
	if orderKey < filterKey {
		t.Fatalf("documentation findings index should put equality filter keys before observed_at: %s", indexSQL)
	}
}

func TestBootstrapDefinitionsIncludePackageCorrelationFactIndexes(t *testing.T) {
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
		"fact_records_active_package_dependency_entity_idx",
		"(payload->'entity_metadata'->>'package_manager')",
		"(payload->>'entity_name')",
		"payload->'entity_metadata'->>'config_kind' = 'dependency'",
		"fact_records_package_correlations_v2_lookup_idx",
		"fact_records_package_correlations_v2_repository_lookup_idx",
		"'reducer_package_ownership_correlation'",
		"'reducer_package_consumption_correlation'",
		"'reducer_package_publication_correlation'",
		"(payload->>'relationship_kind')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
	for _, oldName := range []string{
		"fact_records_package_correlations_lookup_idx",
		"fact_records_package_correlations_repository_lookup_idx",
	} {
		if strings.Contains(facts.SQL, oldName) {
			t.Fatalf("fact_records SQL must not change existing partial index %q in place", oldName)
		}
	}
}

func TestBootstrapDefinitionsIncludeCICDRunCorrelationFactIndexes(t *testing.T) {
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
		"fact_records_ci_cd_run_correlations_lookup_idx",
		"fact_records_ci_cd_run_correlations_run_lookup_idx",
		"fact_records_ci_cd_run_correlations_commit_lookup_idx",
		"fact_records_ci_cd_run_correlations_artifact_lookup_idx",
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
		"fact_records_container_image_identity_digest_idx",
		"'reducer_ci_cd_run_correlation'",
		"'reducer_container_image_identity'",
		"(payload->>'repository_id')",
		"(payload->>'commit_sha')",
		"(payload->>'artifact_digest')",
		"(payload->>'environment')",
		"(payload->>'provider')",
		"(payload->>'run_id')",
		"(payload->>'digest')",
		"fact_id ASC",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
}

func TestBootstrapDefinitionsIncludeSBOMAttestationAttachmentFactIndexes(t *testing.T) {
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
		"fact_records_oci_image_referrer_subject_idx",
		"fact_records_sbom_attestation_attachments_subject_idx",
		"fact_records_sbom_attestation_attachments_document_idx",
		"fact_records_sbom_attestation_attachments_document_digest_idx",
		"fact_records_sbom_attestation_attachments_status_idx",
		"'oci_registry.image_referrer'",
		"'reducer_sbom_attestation_attachment'",
		"(payload->>'subject_digest')",
		"(payload->>'document_id')",
		"(payload->>'document_digest')",
		"(payload->>'attachment_status')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
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

func TestApplyBootstrapExecutesDefinitionsInOrder(t *testing.T) {
	t.Parallel()

	exec := &recordingExecutor{}
	if err := ApplyBootstrap(context.Background(), exec); err != nil {
		t.Fatalf("ApplyBootstrap() error = %v, want nil", err)
	}

	got := exec.statements
	defs := BootstrapDefinitions()
	want := make([]string, 0, len(defs))
	for _, def := range defs {
		want = append(want, def.SQL)
	}
	if len(got) != len(want) {
		t.Fatalf("ApplyBootstrap() executed %d statements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statement[%d] mismatch\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

func TestBootstrapSQLFilesMirrorDefinitions(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", "..", ".."))
	for _, def := range BootstrapDefinitions() {
		path := filepath.Join(repoRoot, def.Path)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if strings.TrimSpace(string(got)) != strings.TrimSpace(def.SQL) {
			t.Fatalf("file %q does not match bootstrap definition %q", path, def.Name)
		}
	}
}

func TestValidateDefinitionsRejectsBlankValues(t *testing.T) {
	t.Parallel()

	err := ValidateDefinitions([]Definition{{Name: " ", Path: "x.sql", SQL: "SELECT 1;"}})
	if err == nil {
		t.Fatal("ValidateDefinitions() error = nil, want non-nil")
	}
}

type recordingExecutor struct {
	statements []string
}

func (e *recordingExecutor) ExecContext(_ context.Context, statement string, _ ...any) (sql.Result, error) {
	e.statements = append(e.statements, statement)
	return result{}, nil
}

type result struct{}

func (result) LastInsertId() (int64, error) { return 0, nil }

func (result) RowsAffected() (int64, error) { return 0, nil }
