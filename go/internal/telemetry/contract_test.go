package telemetry

import (
	"slices"
	"testing"
)

func TestMetricDimensionKeys(t *testing.T) {
	t.Parallel()

	want := []string{
		"scope_id",
		"scope_kind",
		"source",
		"source_system",
		"generation_id",
		"collector_kind",
		"domain",
		"partition_key",
		"runner",
		"lookup_result",
		"error_type",
		"repo_size_tier",
		"skip_reason",
		"node_type",
		"edge_type",
		"write_phase",
		"outcome",
		"backend_kind",
		"result",
		"reason",
		"kind",
		"action",
		"provider",
		"event_kind",
		"decision",
		"status",
		"operation",
		"status_class",
		"failure_class",
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
		"reducer.drift_evidence_load",
		"reducer.aws_runtime_drift_evidence_load",
		"canonical.write",
		"canonical.projection",
		"canonical.retract",
		"ingestion.evidence_discovery",
		"iac_reachability.materialize",
		"reducer.sql_relationship_materialization",
		"reducer.inheritance_materialization",
		"reducer.cross_repo_resolution",
		"shared_acceptance.lookup",
		"shared_acceptance.upsert",
		"query.relationship_evidence",
		"query.evidence_citation_packet",
		"query.documentation_findings",
		"query.documentation_evidence_packet",
		"query.documentation_packet_freshness",
		"query.dead_iac",
		"query.iac_unmanaged_resources",
		"query.iac_management_status",
		"query.iac_management_explanation",
		"query.infra_resource_search",
		"query.code_topic_investigation",
		"query.hardcoded_secret_investigation",
		"query.dead_code_investigation",
		"query.change_surface_investigation",
		"query.resource_investigation",
		"query.package_registry_packages",
		"query.package_registry_versions",
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
		"aws.collector.claim.process",
		"aws.credentials.assume_role",
		"aws.service.scan",
		"aws.service.pagination.page",
		"postgres.exec",
		"postgres.query",
		"neo4j.execute",
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
	}

	got := LogKeys()
	if !slices.Equal(got, want) {
		t.Fatalf("LogKeys() = %v, want %v", got, want)
	}
}
