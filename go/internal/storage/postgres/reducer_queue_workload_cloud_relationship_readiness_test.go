// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueClaimQueryGatesWorkloadCloudRelationshipOnCloudReadiness(t *testing.T) {
	t.Parallel()

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkQuery,
		string(reducer.DomainWorkloadCloudRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("claim query missing workload-cloud readiness requirement:\n%s", claimReducerWorkQuery)
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

	if !queryHasBoundedReadinessRequirement(
		claimReducerWorkBatchQuery,
		string(reducer.DomainWorkloadCloudRelationshipMaterialization),
		"cloud_resource_uid",
		"canonical_nodes_committed",
	) {
		t.Fatalf("batch claim query missing workload-cloud readiness requirement:\n%s", claimReducerWorkBatchQuery)
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
