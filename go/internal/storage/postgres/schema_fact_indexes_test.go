// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

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

func TestBootstrapDefinitionsIncludeCollectorStatusFactIndex(t *testing.T) {
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
		"fact_records_collector_status_active_idx",
		"scope_id,",
		"generation_id,",
		"source_system,",
		"fact_kind,",
		"observed_at DESC",
		"ingested_at DESC",
		"WHERE is_tombstone = FALSE",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing collector-status index marker %q", want)
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
		"fact_records_documentation_sources_observed_idx",
		"fact_records_documentation_findings_visible_idx",
		"fact_records_documentation_packets_finding_idx",
		"fact_records_documentation_packets_packet_idx",
		"ON fact_records (observed_at DESC, fact_id DESC)",
		"WHERE fact_kind = 'documentation_source'",
		"WHERE fact_kind = 'documentation_finding'",
		"WHERE fact_kind = 'documentation_evidence_packet'",
		"viewer_can_read_source",
		"source_acl_evaluated",
		"permission_decision",
		"payload->>'finding_id'",
		"(payload->>'packet_id')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
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
		"fact_records_ci_cd_run_correlations_image_ref_idx",
		"fact_records_ci_cd_run_correlations_environment_lookup_idx",
		"fact_records_container_image_identity_digest_idx",
		"fact_records_container_image_identity_ref_idx",
		"fact_records_container_image_identity_repository_idx",
		"fact_records_container_image_identity_outcome_idx",
		"fact_records_active_container_image_refs_idx",
		"'reducer_ci_cd_run_correlation'",
		"'reducer_container_image_identity'",
		"'aws_image_reference'",
		"'aws_relationship'",
		"'oci_registry.image_tag_observation'",
		"'oci_registry.image_manifest'",
		"'oci_registry.image_index'",
		"payload->'entity_metadata' ? 'container_images'",
		"(payload->>'repository_id')",
		"(payload->>'commit_sha')",
		"(payload->>'artifact_digest')",
		"(payload->>'image_ref')",
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

	containerRefsIndex := `
CREATE INDEX IF NOT EXISTS fact_records_active_container_image_refs_idx
    ON fact_records (
        observed_at ASC,
        fact_id ASC,
        generation_id,
        source_system
    )
    WHERE is_tombstone = FALSE`
	if !strings.Contains(facts.SQL, containerRefsIndex) {
		t.Fatalf("fact_records active container image refs index must start with cursor keys:\n%s", facts.SQL)
	}
}

func TestBootstrapDefinitionsIncludeKubernetesLivePodTemplateObjectFactIndex(t *testing.T) {
	t.Parallel()

	var found Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "kubernetes_live_pod_template_object_index" {
			found = def
			break
		}
	}
	if found.Name == "" {
		t.Fatal("kubernetes_live_pod_template_object_index definition missing; " +
			"create migration 075_kubernetes_live_pod_template_object_index.sql")
	}
	const want = `CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_kubernetes_live_pod_template_object_idx
    ON fact_records (
        (payload->>'group_version_resource'),
        (payload->>'namespace'),
        (payload->>'name')
    )
    WHERE fact_kind = 'kubernetes_live.pod_template'
      AND is_tombstone = FALSE;`
	if !strings.Contains(found.SQL, want) {
		t.Fatalf("kubernetes_live_pod_template_object_index SQL missing exact DDL:\n%s", found.SQL)
	}
}

// TestFactRecordSchemaDroppedStableKeyIndex asserts that factRecordSchemaSQL
// no longer creates fact_records_stable_key_idx but still creates the
// neighboring collector_status_active_idx and active_repository_idx.
func TestFactRecordSchemaDroppedStableKeyIndex(t *testing.T) {
	t.Parallel()

	if strings.Contains(factRecordSchemaSQL, "fact_records_stable_key_idx") {
		t.Fatal("factRecordSchemaSQL must not contain fact_records_stable_key_idx; " +
			"remove the CREATE INDEX lines from schema_fact_records.go")
	}

	// Neighbor indexes must still be present.
	for _, want := range []string{
		"fact_records_collector_status_active_idx",
		"fact_records_active_repository_idx",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing neighbor index %q", want)
		}
	}
}

// TestBootstrapDefinitionsDropFactRecordsStableKeyIndex asserts that
// migration 049_drop_fact_records_stable_key_idx exists and contains
// exactly the CONCURRENTLY drop for the unused index.
func TestBootstrapDefinitionsDropFactRecordsStableKeyIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_fact_records_stable_key_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("drop_fact_records_stable_key_idx definition missing; " +
			"create migration 049_drop_fact_records_stable_key_idx.sql")
	}
	const want = "DROP INDEX CONCURRENTLY IF EXISTS fact_records_stable_key_idx"
	if !strings.Contains(marker.SQL, want) {
		t.Fatalf("drop stable-key-index migration missing %q:\n%s", want, marker.SQL)
	}
}
