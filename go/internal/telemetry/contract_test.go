// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"
)

func TestMetricDimensionKeys(t *testing.T) {
	t.Parallel()

	want := []string{
		"scope_kind",
		"source",
		"source_class",
		"language",
		"source_system",
		"collector_kind",
		"analyzer",
		"target_kind",
		"limit_kind",
		"domain",
		"partition_key",
		"partition_id",
		"runner",
		"lookup_result",
		"error_type",
		"repo_size_tier",
		"skip_reason",
		"node_type",
		"edge_type",
		"write_phase",
		"outcome",
		"guardrail",
		"policy_id",
		"evidence_class",
		"backend_kind",
		"confidence",
		"risk_type",
		"severity",
		"result",
		"reason",
		"kind",
		"action",
		"mcp_method",
		"auth_path",
		"provider",
		"provider_kind",
		"provider_profile_class",
		"event_kind",
		"decision",
		"status",
		"operation",
		"gate",
		"status_class",
		"route",
		"failure_class",
		"fact_kind",
		"resource_scope",
		"document_type",
		"service",
		"account",
		"region",
		"media_family",
		"artifact_family",
		"safe_locator_hash",
		"warning_kind",
		"pack",
		"rule",
		"drift_kind",
		"resource_type",
		"relationship_type",
		"join_mode",
		"family",
		"coverage_signal",
		"resolution_mode",
		"endpoint_kind",
		"principal_kind",
		"budget_state",
		"budget_reason",
		"node_label",
		"age_bucket",
		"ecosystem",
		"cloudformation_section",
		"source_file_kind",
		"bootstrap_phase",
		"stage",
		"source_tool",
		"field_class",
	}

	got := MetricDimensionKeys()
	if !slices.Equal(got, want) {
		t.Fatalf("MetricDimensionKeys() = %v, want %v", got, want)
	}

	got[0] = "mutated"
	if slices.Equal(MetricDimensionKeys(), got) {
		t.Fatalf("MetricDimensionKeys() returned shared storage")
	}
}

func TestSpanNames(t *testing.T) {
	t.Parallel()

	want := []string{
		"collector.observe",
		"collector.stream",
		"scope.assign",
		"fact.emit",
		"projector.run",
		"reducer_intent.enqueue",
		"reducer.run",
		"reducer.batch_claim",
		"semantic_extraction.queue.apply",
		"semantic_extraction.queue.claim",
		"semantic_extraction.queue.complete",
		"reducer.eshu_search_index_write",
		"reducer.drift_evidence_load",
		"reducer.aws_runtime_drift_evidence_load",
		"reducer.multi_cloud_runtime_drift_evidence_load",
		"reducer.aws_relationship_materialization",
		"reducer.gcp_relationship_materialization",
		"reducer.azure_relationship_materialization",
		"reducer.observability_coverage_materialization",
		"reducer.security_group_reachability_materialization",
		"reducer.iam_can_assume_materialization",
		"reducer.s3_logs_to_materialization",
		"reducer.s3_external_principal_grant_materialization",
		"reducer.rds_posture_materialization",
		"reducer.ec2_uses_profile_materialization",
		"reducer.iam_instance_profile_role_materialization",
		"reducer.ec2_internet_exposure_materialization",
		"reducer.ec2_block_device_kms_posture_materialization",
		"reducer.s3_internet_exposure_materialization",
		"reducer.iam_escalation_materialization",
		"reducer.iam_can_perform_materialization",
		"reducer.secrets_iam_graph_projection",
		"canonical.write",
		"canonical.projection",
		"canonical.retract",
		"ingestion.evidence_discovery",
		"iac_reachability.materialize",
		"reducer.sql_relationship_materialization",
		"reducer.inheritance_materialization",
		"reducer.cross_repo_resolution",
		"reducer.code_import_repo_edge",
		"shared_acceptance.lookup",
		"shared_acceptance.upsert",
		"query.relationship_evidence",
		"query.admission_decisions",
		"query.evidence_citation_packet",
		"query.documentation_findings",
		"query.documentation_facts",
		"query.documentation_evidence_packet",
		"query.documentation_packet_freshness",
		"query.semantic_evidence",
		"query.semantic_search",
		"query.documentation_aggregate",
		"query.dead_iac",
		"query.iac_unmanaged_resources",
		"query.iac_management_status",
		"query.iac_management_explanation",
		"query.iac_terraform_import_plan",
		"query.aws_runtime_drift_findings",
		"query.terraform_config_state_drift_findings",
		"query.replatforming_selectors",
		"query.replatforming_plan",
		"query.iac_resources",
		"query.infra_resource_search",
		"query.infra_relationships",
		"query.infra_resource_aggregate",
		"query.cloud_resource_list",
		"query.cloud_inventory_readback",
		"query.cloud_runtime_drift_findings",
		"query.code_topic_investigation",
		"query.hardcoded_secret_investigation",
		"query.dead_code_investigation",
		"query.call_graph_metrics",
		"query.graph_summary_packet",
		"query.graph_entity_inventory",
		"query.change_surface_investigation",
		"query.resource_investigation",
		"query.package_registry_packages",
		"query.package_registry_versions",
		"query.package_registry_dependencies",
		"query.package_registry_correlations",
		"query.package_registry_dependency_chains",
		"query.package_registry_aggregate",
		"query.ci_cd_run_correlations",
		"query.service_catalog_correlations",
		"query.kubernetes_correlations",
		"query.observability_coverage_correlations",
		"query.secrets_iam_identity_trust_chains",
		"query.secrets_iam_privilege_posture_observations",
		"query.secrets_iam_secret_access_paths",
		"query.secrets_iam_posture_gaps",
		"query.secrets_iam_posture_summary",
		"query.container_image_identities",
		"query.supply_chain_security_alerts",
		"query.sbom_attestation_attachments",
		"query.advisory_evidence",
		"query.incident_context",
		"query.work_item_evidence",
		"query.freshness_generation_lifecycle",
		"query.freshness_changed_since",
		"query.freshness_service_changed_since",
		"query.advisory_catalog",
		"vulnerability_intelligence.observe",
		"vulnerability_intelligence.fetch",
		"security_alert.observe",
		"security_alert.fetch",
		"pagerduty.observe",
		"pagerduty.fetch",
		"jira.observe",
		"jira.fetch",
		"grafana.observe",
		"grafana.fetch",
		"prometheus_mimir.observe",
		"prometheus_mimir.fetch",
		"loki.observe",
		"loki.fetch",
		"tempo.observe",
		"tempo.fetch",
		"query.supply_chain_impact_findings",
		"query.supply_chain_impact_explanation",
		"query.supply_chain_impact_aggregate",
		"query.security_alert_reconciliation_aggregate",
		"query.container_image_identity_aggregate",
		"query.sbom_attestation_attachment_aggregate",
		"query.ci_cd_run_correlation_aggregate",
		"query.dependencies",
		"query.codeowners_ownership",
		"query.container_image_tag_history",
		"scanner_worker.claim.process",
		"scanner_worker.analyze",
		"scanner_worker.fact.emit_batch",
		"tfstate.collector.claim.process",
		"tfstate.discovery.resolve",
		"tfstate.source.open",
		"tfstate.parser.stream",
		"tfstate.fact.emit_batch",
		"tfstate.coordinator.complete",
		"webhook.handle",
		"webhook.store",
		"oci_registry.scan",
		"oci_registry.api_call",
		"kubernetes_live.snapshot",
		"kubernetes_live.api_call",
		"vault_live.snapshot",
		"ci_cd_run.observe",
		"ci_cd_run.fetch",
		"aws.collector.claim.process",
		"aws.credentials.assume_role",
		"aws.service.scan",
		"aws.service.pagination.page",
		"postgres.exec",
		"postgres.query",
		"neo4j.execute",
		"bootstrap.collector_cycle",
		"collector.claimed_run",
		"collector.snapshot_stage",
	}

	got := SpanNames()
	if !slices.Equal(got, want) {
		t.Fatalf("SpanNames() = %v, want %v", got, want)
	}
}

func TestLogKeys(t *testing.T) {
	t.Parallel()

	want := []string{
		"scope_id",
		"scope_kind",
		"source_system",
		"generation_id",
		"collector_kind",
		"domain",
		"partition_key",
		"request_id",
		"failure_class",
		"refresh_skipped",
		"pipeline_phase",
		"acceptance.scope_id",
		"acceptance.unit_id",
		"acceptance.source_run_id",
		"acceptance.generation_id",
		"acceptance.stale_count",
		"resource.fingerprint",
		"resource.identity_kind",
		"resource.type",
		"depth",
		"prior_config_addresses",
		"state_only_addresses",
		"addresses_promoted_to_removed_from_config",
		"multi_element.prefix",
		"multi_element.count",
		"multi_element.source",
		"resource_type",
		"attribute_key",
		"path",
		"error",
		"semantic_extraction.status",
		"semantic_extraction.source_class",
		"semantic_extraction.provider_kind",
		"semantic_extraction.provider_profile_class",
		"semantic_extraction.budget_state",
		"semantic_extraction.budget_reason",
	}

	got := LogKeys()
	if !slices.Equal(got, want) {
		t.Fatalf("LogKeys() = %v, want %v", got, want)
	}
}

func TestAttrSourceTool(t *testing.T) {
	t.Parallel()
	attr := AttrSourceTool("terraform")
	if string(attr.Key) != MetricDimensionSourceTool {
		t.Fatalf("AttrSourceTool key = %q, want %q", attr.Key, MetricDimensionSourceTool)
	}
	if attr.Value.AsString() != "terraform" {
		t.Fatalf("AttrSourceTool value = %q, want %q", attr.Value.AsString(), "terraform")
	}
}

func TestMetricDimensionSourceToolValue(t *testing.T) {
	t.Parallel()
	if MetricDimensionSourceTool != "source_tool" {
		t.Fatalf("MetricDimensionSourceTool = %q, want %q", MetricDimensionSourceTool, "source_tool")
	}
}
