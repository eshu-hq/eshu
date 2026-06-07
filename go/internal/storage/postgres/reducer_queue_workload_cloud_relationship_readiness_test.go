package postgres

import (
	"strings"
	"testing"
)

func TestReducerQueueClaimQueryGatesWorkloadCloudRelationshipOnCloudReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"workload_cloud_relationship_materialization",
		"aws_nodes.keyspace = 'cloud_resource_uid'",
		"aws_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(claimReducerWorkQuery, want) {
			t.Fatalf("claim query missing workload-cloud readiness token %q:\n%s", want, claimReducerWorkQuery)
		}
	}
	for _, blocked := range []string{
		"graph_projection_phase_state AS workload_nodes",
		"workload_nodes.keyspace = 'service_uid'",
		"workload_nodes.phase = 'workload_materialization'",
	} {
		if strings.Contains(claimReducerWorkQuery, blocked) {
			t.Fatalf("claim query has stale workload readiness token %q:\n%s", blocked, claimReducerWorkQuery)
		}
	}
}

func TestReducerQueueBatchClaimQueryGatesWorkloadCloudRelationshipOnCloudReadiness(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"workload_cloud_relationship_materialization",
		"same_nodes.keyspace = 'cloud_resource_uid'",
		"same_nodes.phase = 'canonical_nodes_committed'",
	} {
		if !strings.Contains(claimReducerWorkBatchQuery, want) {
			t.Fatalf("batch claim query missing workload-cloud readiness token %q:\n%s", want, claimReducerWorkBatchQuery)
		}
	}
	for _, blocked := range []string{
		"graph_projection_phase_state AS same_workload_nodes",
		"same_workload_nodes.keyspace = 'service_uid'",
		"same_workload_nodes.phase = 'workload_materialization'",
	} {
		if strings.Contains(claimReducerWorkBatchQuery, blocked) {
			t.Fatalf("batch claim query has stale workload readiness token %q:\n%s", blocked, claimReducerWorkBatchQuery)
		}
	}
}
