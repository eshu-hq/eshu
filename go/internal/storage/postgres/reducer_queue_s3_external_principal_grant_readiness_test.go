package postgres

import (
	"strings"
	"testing"
)

func TestReducerQueueClaimQueryGatesS3ExternalPrincipalGrantOnCloudResourceReadiness(t *testing.T) {
	t.Parallel()

	required := []string{
		"s3_external_principal_grant_materialization",
		"graph_projection_phase_state AS aws_nodes",
		"aws_nodes.keyspace = 'cloud_resource_uid'",
		"aws_nodes.phase = 'canonical_nodes_committed'",
	}
	for _, want := range required {
		if !strings.Contains(claimReducerWorkQuery, want) {
			t.Fatalf("claimReducerWorkQuery missing %q", want)
		}
		if !strings.Contains(claimReducerWorkBatchQuery, want) {
			t.Fatalf("claimReducerWorkBatchQuery missing %q", want)
		}
	}
	if !strings.Contains(reducerConflictBlockageQuery, "s3_external_principal_grant_materialization") {
		t.Fatalf("reducerConflictBlockageQuery missing s3 external-principal readiness blockage domain")
	}
}
