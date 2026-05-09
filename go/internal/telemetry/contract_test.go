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
		"query.documentation_findings",
		"query.documentation_evidence_packet",
		"query.documentation_packet_freshness",
		"query.dead_iac",
		"query.infra_resource_search",
		"tfstate.collector.claim.process",
		"tfstate.discovery.resolve",
		"tfstate.source.open",
		"tfstate.parser.stream",
		"tfstate.fact.emit_batch",
		"tfstate.coordinator.complete",
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
	}

	got := LogKeys()
	if !slices.Equal(got, want) {
		t.Fatalf("LogKeys() = %v, want %v", got, want)
	}
}
