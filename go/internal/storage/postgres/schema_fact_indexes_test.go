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
		"fact_records_documentation_findings_visible_idx",
		"fact_records_documentation_sources_observed_idx",
		"fact_records_documentation_packets_finding_idx",
		"fact_records_documentation_packets_packet_idx",
		"ON fact_records (observed_at DESC, fact_id DESC)",
		"WHERE fact_kind = 'documentation_source'",
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
