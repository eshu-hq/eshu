// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// Shared fixtures for the #7666 aws_cloud_runtime_drift state-join proofs:
// the plan proof (aws_cloud_runtime_drift_state_arn_plan_live_test.go) and the
// row differential (aws_cloud_runtime_drift_state_arn_differential_live_test.go).
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/storage/postgres -run 'AWSCloudRuntimeDriftStateARN' -count=1 -v

// awsDriftStateARNIndexName is the migration 180 partial expression index the
// drift state join must probe (#7666).
const awsDriftStateARNIndexName = "fact_records_terraform_state_resource_arn_idx"

// awsDriftARNPlanNode is the slice of an EXPLAIN (ANALYZE, BUFFERS, FORMAT
// JSON) node the #7666 plan proof reads.
type awsDriftARNPlanNode struct {
	NodeType         string                `json:"Node Type"`
	RelationName     string                `json:"Relation Name"`
	IndexName        string                `json:"Index Name"`
	ActualLoops      float64               `json:"Actual Loops"`
	ActualRows       float64               `json:"Actual Rows"`
	SharedHitBlocks  int64                 `json:"Shared Hit Blocks"`
	SharedReadBlocks int64                 `json:"Shared Read Blocks"`
	Plans            []awsDriftARNPlanNode `json:"Plans"`
}

// walk visits the node and every descendant.
func (n awsDriftARNPlanNode) walk(visit func(awsDriftARNPlanNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}

// awsDriftARNFixture is the seeded shape the plan proof shares.
type awsDriftARNFixture struct {
	awsScopeID      string
	awsGenerationID string
	arns            []string
	wantRows        int
}

// awsDriftARNFixtureGenerations is the number of retained generations each
// state scope holds; the third is active.
const awsDriftARNFixtureGenerations = 3

// seedAWSDriftStateARNFixture seeds stateScopes Terraform-state scopes with
// three generations each (the third active), 24 terraform_state_resource rows
// per generation where every fourth row carries an ARN and every
// fourth-plus-two row a whitespace-only ARN, plus one AWS scope whose sealed
// generation holds 120 ARNs: odd k names an ARN some active state generation
// declares, even k names none.
//
// With staleStatistics false it ANALYZEs after the load, so the planner sees
// the fixture's real distribution. With it true it ANALYZEs before the state
// rows land and turns autovacuum off for this schema's fact_records, so the
// planner believes no terraform_state_resource rows exist: the shape a
// production table is in between autovacuum ANALYZE runs after a large state
// ingest (a 36.9M-row table re-analyzes only after about 3.7M changed rows).
func seedAWSDriftStateARNFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	stateScopes int,
	staleStatistics bool,
) awsDriftARNFixture {
	t.Helper()
	const awsScope, awsGeneration, awsResources = "aws:7666:us-east-1", "aws:7666:g1", 120
	setup := []string{
		fmt.Sprintf(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id, payload)
SELECT 'state_snapshot:s3:h' || i, 'state_snapshot', 'terraform_state', 's3://b/' || i, 'terraform_state',
       'p' || i, now(), now(), 'active', 'state:' || i || ':g3', '{}'::jsonb
FROM generate_series(1, %d) AS i`, stateScopes),
		fmt.Sprintf(`INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
   observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('%s', 'aws_account_region', 'aws', 'aws/7666', 'aws', 'p0', now(), now(), 'active', '%s', '{}'::jsonb)`,
			awsScope, awsGeneration),
		fmt.Sprintf(`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
SELECT 'state:' || i || ':g' || g, 'state_snapshot:s3:h' || i, 'snapshot',
       now() - ((4 - g) || ' hours')::interval, now(),
       CASE WHEN g = 3 THEN 'active' ELSE 'superseded' END, '{}'::jsonb
FROM generate_series(1, %d) AS i, generate_series(1, %d) AS g`, stateScopes, awsDriftARNFixtureGenerations),
		fmt.Sprintf(`INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
VALUES ('%s', '%s', 'snapshot', now(), now(), 'active', '{}'::jsonb)`, awsGeneration, awsScope),
		fmt.Sprintf(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'aws:' || k, '%s', '%s', 'aws_resource', 'a' || k, 'aws', 'sa' || k, now(), now(),
       jsonb_build_object('arn', CASE WHEN k %% 2 = 1
           THEN 'arn:aws:s3:::b-' || (1 + (k * 7) %% %d) || '-' || (4 * (1 + k %% 6))
           ELSE 'arn:aws:s3:::unmatched-' || k END,
         'resource_type', 'aws_s3_bucket')
FROM generate_series(1, %d) AS k`, awsScope, awsGeneration, stateScopes, awsResources),
	}
	stateRows := fmt.Sprintf(`INSERT INTO fact_records
  (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
   source_fact_key, observed_at, ingested_at, payload)
SELECT 'tf:' || i || ':' || g || ':' || j, 'state_snapshot:s3:h' || i, 'state:' || i || ':g' || g,
       'terraform_state_resource', 'k' || i || ':' || j, 'terraform_state', 'sk' || i || ':' || j, now(), now(),
       jsonb_build_object(
         'address', 'aws_s3_bucket.r' || j, 'type', 'aws_s3_bucket', 'mode', 'managed',
         'attributes', CASE
           WHEN j %% 4 = 0 THEN jsonb_build_object('arn', 'arn:aws:s3:::b-' || i || '-' || j, 'id', 'b-' || i || '-' || j)
           WHEN j %% 4 = 2 THEN jsonb_build_object('arn', '   ', 'id', 'r-' || i || '-' || j)
           ELSE jsonb_build_object('id', 'r-' || i || '-' || j) END)
FROM generate_series(1, %d) AS i, generate_series(1, %d) AS g, generate_series(1, 24) AS j`,
		stateScopes, awsDriftARNFixtureGenerations)
	analyze := []string{`ANALYZE ingestion_scopes`, `ANALYZE scope_generations`, `ANALYZE fact_records`}

	var statements []string
	if staleStatistics {
		statements = append(statements, `ALTER TABLE fact_records SET (autovacuum_enabled = false)`)
		statements = append(statements, setup...)
		statements = append(statements, analyze...)
		statements = append(statements, stateRows)
	} else {
		statements = append(statements, setup...)
		statements = append(statements, stateRows)
		statements = append(statements, analyze...)
	}
	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed #7666 drift ARN fixture: %v\n%s", err, statement)
		}
	}

	arnSet := map[string]bool{}
	matched := map[string]bool{}
	for k := 1; k <= awsResources; k++ {
		if k%2 == 1 {
			arn := fmt.Sprintf("arn:aws:s3:::b-%d-%d", 1+(k*7)%stateScopes, 4*(1+k%6))
			arnSet[arn] = true
			matched[arn] = true
			continue
		}
		arnSet[fmt.Sprintf("arn:aws:s3:::unmatched-%d", k)] = true
	}
	arns := make([]string, 0, len(arnSet))
	for arn := range arnSet {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	// Each matched ARN is declared by exactly one active state generation.
	return awsDriftARNFixture{awsScopeID: awsScope, awsGenerationID: awsGeneration, arns: arns, wantRows: len(matched)}
}

// awsDriftQuoteLiteral renders a test-controlled value as a SQL string
// literal.
func awsDriftQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// explainAWSDriftStateARNJoin runs the shipped state-join statement under
// EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) on one pinned connection. generic
// forces the generic plan pgx's statement cache can settle on after five
// executions; otherwise the statement gets the custom plan of its first runs.
func explainAWSDriftStateARNJoin(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	fixture awsDriftARNFixture,
	generic bool,
) awsDriftARNPlanNode {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("pin connection: %v", err)
	}
	defer func() { _ = conn.Close() }()

	encoded, err := json.Marshal(fixture.arns)
	if err != nil {
		t.Fatalf("marshal arns: %v", err)
	}
	statement := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + listActiveStateResourcesForAWSARNsQuery
	args := []any{fixture.awsScopeID, fixture.awsGenerationID, string(encoded)}
	if generic {
		if _, err := conn.ExecContext(ctx, "PREPARE aws_drift_state_arn_7666(text, text, jsonb) AS "+listActiveStateResourcesForAWSARNsQuery); err != nil {
			t.Fatalf("prepare state join: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE aws_drift_state_arn_7666") }()
		if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
			t.Fatalf("force generic plan: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "RESET plan_cache_mode") }()
		// EXPLAIN EXECUTE takes no bind parameters, so the fixture's own
		// values go in as quoted literals.
		statement = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE aws_drift_state_arn_7666(" +
			awsDriftQuoteLiteral(fixture.awsScopeID) + ", " +
			awsDriftQuoteLiteral(fixture.awsGenerationID) + ", " +
			awsDriftQuoteLiteral(string(encoded)) + ")"
		args = nil
	}

	var raw string
	if err := conn.QueryRowContext(ctx, statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain state join (generic=%t): %v", generic, err)
	}
	var plans []struct {
		Plan awsDriftARNPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode explain json (generic=%t): %v\n%s", generic, err, raw)
	}
	return plans[0].Plan
}
