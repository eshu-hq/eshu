package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReducerQueueBlockagesReportAWSRelationshipReadinessWait(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{rows: nil}}}
	if _, err := listReducerConflictBlockages(
		context.Background(),
		queryer,
		time.Date(2026, time.May, 31, 10, 30, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("listReducerConflictBlockages() error = %v", err)
	}
	if len(queryer.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(queryer.queries))
	}

	query := queryer.queries[0]
	for _, want := range []string{
		"active_fact_work_items AS (",
		"FROM fact_work_items AS work",
		"JOIN ingestion_scopes AS scope",
		"scope.active_generation_id = active_generation.generation_id",
		"work.stage = 'reducer'",
		"work.status IN ('pending', 'retrying', 'failed', 'dead_letter')",
		"stale_generation.ingested_at < active_generation.ingested_at",
		"stale_generation.generation_id < active_generation.generation_id",
		"FROM active_fact_work_items",
		"readiness_blocked AS (",
		"eligible.domain IN ('aws_relationship_materialization', 'observability_coverage_materialization', 'iam_can_assume_materialization', 's3_logs_to_materialization', 's3_external_principal_grant_materialization', 'rds_posture_materialization', 'iam_instance_profile_role_materialization', 'ec2_internet_exposure_materialization', 's3_internet_exposure_materialization')",
		"FROM graph_projection_phase_state AS aws_nodes",
		"aws_nodes.acceptance_unit_id = COALESCE(NULLIF(eligible.payload->>'entity_key', ''), eligible.scope_id)",
		"aws_nodes.keyspace = 'cloud_resource_uid'",
		"aws_nodes.phase = 'canonical_nodes_committed'",
		"'readiness' AS conflict_domain",
		"'cloud_resource_uid:canonical_nodes_committed:'",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("blockage query missing AWS relationship readiness diagnostic %q:\n%s", want, query)
		}
	}
}
