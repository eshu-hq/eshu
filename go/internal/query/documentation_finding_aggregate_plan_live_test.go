// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const documentationAggregateVisibleIndexName = "fact_records_documentation_findings_visible_idx"

// TestDocumentationFindingAggregateBuildersUseVisibleIndexLive binds the
// production total, grouped, and inventory SQL builders to a representative
// ACL-skewed PostgreSQL plan. The disposable proof is opt-in because it creates
// 200,000 rows and executes the real bootstrap DDL.
func TestDocumentationFindingAggregateBuildersUseVisibleIndexLive(t *testing.T) {
	ctx, db := postgresproof.OpenDisposableDatabase(
		t,
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DSN"),
		os.Getenv("ESHU_TEST_DOCUMENTATION_INDEX_POSTGRES_DISPOSABLE"),
		4*time.Minute,
	)
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply bootstrap: %v", err)
	}
	seedDocumentationFindingAggregatePlanProof(t, ctx, db)

	groupExpr, err := documentationFindingInventoryGroupExpression(DocumentationFindingInventoryByStatus)
	if err != nil {
		t.Fatalf("status group expression: %v", err)
	}
	filter := DocumentationFindingAggregateFilter{}
	totalSQL, totalArgs := buildDocumentationFindingAggregateTotalSQL(filter)
	groupSQL, groupArgs := buildDocumentationFindingAggregateGroupSQL(filter, groupExpr)
	inventorySQL, inventoryArgs := buildDocumentationFindingInventorySQL(filter, groupExpr, 10, 0)
	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{name: "total", query: totalSQL, args: totalArgs},
		{name: "grouped_status", query: groupSQL, args: groupArgs},
		{name: "inventory_status", query: inventorySQL, args: inventoryArgs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertDocumentationAggregateVisibleIndexPlan(t, ctx, db, tc.query, tc.args...)
		})
	}
	assertDocumentationAggregateIdentity(t, ctx, db)
}

func seedDocumentationFindingAggregatePlanProof(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status
) VALUES (
  'scope:documentation-aggregate-plan', 'repository', 'proof', 'proof', 'proof',
  'proof', clock_timestamp(), clock_timestamp(), 'active'
);
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at
) VALUES (
  'generation:documentation-aggregate-plan', 'scope:documentation-aggregate-plan',
  'proof', clock_timestamp(), clock_timestamp(), 'active', clock_timestamp()
);
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  collector_kind, source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'finding:aggregate-plan:' || n,
  'scope:documentation-aggregate-plan', 'generation:documentation-aggregate-plan',
  'documentation_finding', 'finding:aggregate-plan:' || n,
  'proof', 'proof', 'finding:aggregate-plan:' || n,
  clock_timestamp(), clock_timestamp(),
  jsonb_build_object(
    'finding_type', 'documentation_drift',
    'source_id', 'source:aggregate-plan',
    'document_id', 'document:aggregate-plan:' || n,
    'status', CASE WHEN n % 3 = 0 THEN 'open' ELSE 'closed' END,
    'truth_level', 'observed', 'freshness_state', 'fresh',
    'permissions', jsonb_build_object(
      'viewer_can_read_source', n % 20 = 0,
      'source_acl_evaluated', n % 20 = 0
    ),
    'states', jsonb_build_object(
      'permission_decision', CASE WHEN n % 20 = 0 THEN 'allowed' ELSE 'denied' END
    )
  )
FROM generate_series(1, 200000) AS n;
ANALYZE fact_records;
`); err != nil {
		t.Fatalf("seed aggregate plan proof: %v", err)
	}
}

func assertDocumentationAggregateVisibleIndexPlan(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args ...any,
) {
	t.Helper()
	var raw []byte
	if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		t.Fatalf("explain production aggregate builder: %v", err)
	}
	var plans []documentationAggregatePlanResult
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode production aggregate plan: err=%v raw=%s", err, raw)
	}
	indexPlan, ok := findDocumentationAggregateIndexPlan(plans[0].Plan)
	if !ok {
		t.Fatalf("production aggregate plan did not select %s: %s", documentationAggregateVisibleIndexName, raw)
	}
	t.Logf("AGGREGATE_PLAN index=%s execution_ms=%.3f actual_rows=%.0f shared_hits=%d shared_reads=%d", indexPlan.IndexName, plans[0].ExecutionTime, indexPlan.ActualRows, indexPlan.SharedHits, indexPlan.SharedReads)
}

type documentationAggregatePlanResult struct {
	Plan          documentationAggregatePlanNode `json:"Plan"`
	ExecutionTime float64                        `json:"Execution Time"`
}

type documentationAggregatePlanNode struct {
	IndexName   string                           `json:"Index Name"`
	ActualRows  float64                          `json:"Actual Rows"`
	SharedHits  int64                            `json:"Shared Hit Blocks"`
	SharedReads int64                            `json:"Shared Read Blocks"`
	Plans       []documentationAggregatePlanNode `json:"Plans"`
}

func findDocumentationAggregateIndexPlan(node documentationAggregatePlanNode) (documentationAggregatePlanNode, bool) {
	if node.IndexName == documentationAggregateVisibleIndexName {
		return node, true
	}
	for _, child := range node.Plans {
		if found, ok := findDocumentationAggregateIndexPlan(child); ok {
			return found, true
		}
	}
	return documentationAggregatePlanNode{}, false
}

func assertDocumentationAggregateIdentity(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	var visible, reference int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM fact_records
WHERE fact_kind = 'documentation_finding' AND is_tombstone = FALSE
  AND (payload->'permissions'->>'viewer_can_read_source') = 'true'
  AND LOWER(COALESCE(payload->'permissions'->>'source_acl_evaluated', 'true')) <> 'false'
  AND LOWER(COALESCE(payload->'states'->>'permission_decision', '')) <> 'denied'
`).Scan(&reference); err != nil {
		t.Fatalf("reference aggregate count: %v", err)
	}
	query, args := buildDocumentationFindingAggregateTotalSQL(DocumentationFindingAggregateFilter{})
	if err := db.QueryRowContext(ctx, query, args...).Scan(&visible); err != nil {
		t.Fatalf("production aggregate count: %v", err)
	}
	if visible != reference {
		t.Fatalf("production aggregate count = %d, want reference %d", visible, reference)
	}
}
