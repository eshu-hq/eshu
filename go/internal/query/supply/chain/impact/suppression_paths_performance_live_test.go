// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	suppressionPathsSourceRows   = 100_000
	suppressionPathsCanonical    = 50_000
	suppressionPathsOperatorRows = 500
)

func TestSupplyChainSuppressionPathsPerformanceLive(t *testing.T) {
	ctx, db := openSuppressionPathsProofDB(t)
	requiredDefinitions := map[string]bool{
		"ingestion_scopes":                            true,
		"scope_generations":                           true,
		"fact_records":                                true,
		"supply_chain_impact_canonical_winners":       true,
		"supply_chain_impact_winners_materialization": true,
	}
	var definitions []storagepostgres.Definition
	var findingIDIndex storagepostgres.Definition
	for _, definition := range storagepostgres.BootstrapDefinitions() {
		if requiredDefinitions[definition.Name] {
			definitions = append(definitions, definition)
		}
		if definition.Name == "supply_chain_impact_finding_id_index" {
			findingIDIndex = definition
		}
	}
	if len(definitions) != len(requiredDefinitions) {
		t.Fatalf("proof definitions = %d, want %d", len(definitions), len(requiredDefinitions))
	}
	if findingIDIndex.Name == "" {
		t.Fatal("supply-chain impact finding-ID index definition missing")
	}
	if err := storagepostgres.ApplyDefinitions(
		ctx,
		storagepostgres.SQLDB{DB: db},
		definitions,
	); err != nil {
		t.Fatalf("apply Postgres bootstrap schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
ALTER TABLE supply_chain_impact_canonical_winners
  ADD COLUMN IF NOT EXISTS suppression_expires_at TIMESTAMPTZ NULL`); err != nil {
		t.Fatalf("ensure suppression expiry column: %v", err)
	}
	seedSuppressionPathsFacts(t, ctx, db)
	if err := storagepostgres.ApplyDefinitions(
		ctx,
		storagepostgres.SQLDB{DB: db},
		[]storagepostgres.Definition{findingIDIndex},
	); err != nil {
		t.Fatalf("apply populated finding-ID index migration: %v", err)
	}

	winners := storagepostgres.NewSupplyChainImpactWinnersStore(db)
	if err := winners.RebuildAllWinners(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("initial winners rebuild: %v", err)
	}
	var winnerCount int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM supply_chain_impact_canonical_winners`,
	).Scan(&winnerCount); err != nil {
		t.Fatalf("count canonical winners: %v", err)
	}
	if winnerCount != suppressionPathsCanonical {
		t.Fatalf("canonical winners = %d, want %d", winnerCount, suppressionPathsCanonical)
	}

	direct := NewPostgresFindingStore(db)
	materialized := NewPostgresFindingStoreWithReadModel(db, true)
	aggregates := NewPostgresAggregateStore(db)
	listFilter := FindingFilter{
		DetectionProfile:  "precise",
		PriorityBucket:    "high",
		IncludeSuppressed: true,
		Limit:             201,
	}
	directRows, err := direct.ListSupplyChainImpactFindings(ctx, listFilter)
	if err != nil {
		t.Fatalf("list direct findings: %v", err)
	}
	materializedRows, err := materialized.ListSupplyChainImpactFindings(ctx, listFilter)
	if err != nil {
		t.Fatalf("list materialized findings: %v", err)
	}
	if !reflect.DeepEqual(directRows, materializedRows) {
		t.Fatal("direct/materialized result differential is non-zero")
	}
	if len(directRows) != 201 {
		t.Fatalf("list page rows = %d, want 201", len(directRows))
	}

	aggregateFilter := AggregateFilter{
		DetectionProfile:  "precise",
		IncludeSuppressed: true,
	}
	count, err := aggregates.CountSupplyChainImpactFindings(ctx, aggregateFilter)
	if err != nil {
		t.Fatalf("count aggregate findings: %v", err)
	}
	if count.TotalFindings != suppressionPathsCanonical {
		t.Fatalf("aggregate findings = %d, want %d", count.TotalFindings, suppressionPathsCanonical)
	}
	if count.ByPriorityBucket["high"] != suppressionPathsCanonical ||
		count.BySeverity["high"] != suppressionPathsCanonical {
		t.Fatalf(
			"aggregate facets priority/severity = %#v/%#v, want high=%d",
			count.ByPriorityBucket,
			count.BySeverity,
			suppressionPathsCanonical,
		)
	}

	explanation, err := direct.ExplainSupplyChainImpact(
		ctx,
		ExplanationFilter{FindingID: "finding:000501"},
	)
	if err != nil {
		t.Fatalf("explain finding: %v", err)
	}
	if explanation.Finding.FindingID != "finding:000501" {
		t.Fatalf("explain finding_id = %q, want finding:000501", explanation.Finding.FindingID)
	}

	pathRuns := []struct {
		name string
		run  func() error
	}{
		{"direct_list", func() error {
			_, err := direct.ListSupplyChainImpactFindings(ctx, listFilter)
			return err
		}},
		{"materialized_list", func() error {
			_, err := materialized.ListSupplyChainImpactFindings(ctx, listFilter)
			return err
		}},
		{"aggregate_count_and_facets", func() error {
			_, err := aggregates.CountSupplyChainImpactFindings(ctx, aggregateFilter)
			return err
		}},
		{"explain", func() error {
			_, err := direct.ExplainSupplyChainImpact(ctx, ExplanationFilter{FindingID: "finding:000501"})
			return err
		}},
		{"winner_rebuild", func() error {
			return winners.RebuildAllWinners(ctx, time.Now().UTC())
		}},
	}
	for _, pathRun := range pathRuns {
		values := measureSuppressionPath(t, pathRun.run)
		t.Logf("%s 100k-source/50k-canonical/500-operator median=%.3fms p95=%.3fms (reported, never gated; see #7553)",
			pathRun.name, suppressionPathMedian(values), suppressionPathP95(values))
	}

	// Vacuum the rebuild-churned tables so both EXPLAIN runs observe identical
	// heap state instead of racing autovacuum: each rebuild upserts all
	// 50,000 canonical winners, leaving ~150k dead tuples behind (#7553).
	if _, err := db.ExecContext(ctx, `VACUUM (ANALYZE)
supply_chain_impact_canonical_winners,
supply_chain_impact_winners_materialization`); err != nil {
		t.Fatalf("vacuum winners tables: %v", err)
	}

	readAt := time.Now().UTC()
	listArgs := suppressionListPlanArgs(listFilter, readAt)
	aggregateArgs := suppressionAggregatePlanArgs(aggregateFilter, readAt)
	planQueries := []struct {
		name  string
		query string
		args  []any
	}{
		{"direct_list", ListFindingsQuery, listArgs},
		{"materialized_list", ListFindingsFromWinnersQuery, listArgs},
		{"aggregate_count", AggregateCountQuery, aggregateArgs},
		{"aggregate_priority_facet", AggregatePriorityCountQuery, aggregateArgs},
		{"explain", ExplainFindingByPublicIDQuery, suppressionExplainPlanArgs(readAt)},
	}
	for _, planQuery := range planQueries {
		first := explainSuppressionQueryPlan(t, ctx, db, planQuery.query, planQuery.args...)
		second := explainSuppressionQueryPlan(t, ctx, db, planQuery.query, planQuery.args...)
		firstTypes := explainSuppressionPlanNodeTypes(t, ctx, db, planQuery.query, planQuery.args...)
		secondTypes := explainSuppressionPlanNodeTypes(t, ctx, db, planQuery.query, planQuery.args...)
		assertSuppressionPlanShape(t, planQuery.name, first, second, firstTypes, secondTypes)
	}
}

// explainSuppressionPlanNodeTypes returns the sorted whole-tree node-type
// multiset from one EXPLAIN run. The shared helper keeps only the top plan
// node, so this file decodes the tree itself (#7553).
func explainSuppressionPlanNodeTypes(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	var payload []byte
	err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&payload)
	if err != nil {
		t.Fatalf("explain suppression query: %v", err)
	}
	var docs []struct {
		Plan suppressionPlanTree `json:"Plan"`
	}
	if err := json.Unmarshal(payload, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("decode suppression query plan: err=%v", err)
	}
	types := docs[0].Plan.appendTypes(nil)
	sort.Strings(types)
	return types
}

// suppressionPlanTree is one EXPLAIN JSON plan node with its children.
type suppressionPlanTree struct {
	NodeType string                `json:"Node Type"`
	Plans    []suppressionPlanTree `json:"Plans"`
}

func (n suppressionPlanTree) appendTypes(into []string) []string {
	into = append(into, n.NodeType)
	for _, child := range n.Plans {
		into = child.appendTypes(into)
	}
	return into
}

// assertSuppressionPlanShape gates one path on runner-local EXPLAIN runs:
// identical node set, shared touches within 1 percent, and temp touches
// within 1 percent (the #7127 ruling-E shape). Temp is gated on stability,
// not on zero: the direct_list 100k-row dedup sort spills (temp 2192/3381
// at stock 4MB work_mem, measured 2026-10-12), so Temp == 0 cannot gate
// this query. The hit/read split shifts with cache warmth, so the gate
// uses the total.
func assertSuppressionPlanShape(t *testing.T, name string, first, second suppressionQueryPlanProof, firstTypes, secondTypes []string) {
	t.Helper()
	if !slices.Equal(firstTypes, secondTypes) {
		t.Fatalf("%s node set drifted between runs:\nfirst=%v\nsecond=%v", name, firstTypes, secondTypes)
	}
	firstShared := first.Plan.SharedHitBlocks + first.Plan.SharedReadBlocks
	secondShared := second.Plan.SharedHitBlocks + second.Plan.SharedReadBlocks
	assertSuppressionBlocksWithinPercent(t, name, "shared", firstShared, secondShared)
	assertSuppressionBlocksWithinPercent(t, name, "temp_read", first.Plan.TempReadBlocks, second.Plan.TempReadBlocks)
	assertSuppressionBlocksWithinPercent(t, name, "temp_written", first.Plan.TempWrittenBlocks, second.Plan.TempWrittenBlocks)
	t.Logf("%s nodes=%v shared=%d/%d temp_read=%d/%d temp_written=%d/%d",
		name, firstTypes, firstShared, secondShared,
		first.Plan.TempReadBlocks, second.Plan.TempReadBlocks, first.Plan.TempWrittenBlocks, second.Plan.TempWrittenBlocks)
}

func assertSuppressionBlocksWithinPercent(t *testing.T, name, kind string, first, second int64) {
	t.Helper()
	diff := first - second
	if diff < 0 {
		diff = -diff
	}
	if ceiling := max(first, second); ceiling > 0 && diff*100 > ceiling {
		t.Fatalf("%s %s drifted: %d vs %d, want within 1 percent", name, kind, first, second)
	}
}

func openSuppressionPathsProofDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5465 path performance proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	schemaName := fmt.Sprintf("suppression_paths_5465_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+array.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+array.QuoteIdentifier(schemaName)+" CASCADE",
		)
	})

	targetURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse ESHU_POSTGRES_TEST_DSN: %v", err)
	}
	params := targetURL.Query()
	params.Set("search_path", schemaName)
	targetURL.RawQuery = params.Encode()
	db, err := sql.Open("pgx", targetURL.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated Postgres schema: %v", err)
	}
	return ctx, db
}

func seedSuppressionPathsFacts(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, scope := range []struct {
		scopeID, generationID string
	}{
		{"scope:5465:path-proof", "generation:5465:path-proof"},
		{"operator:vulnerability_suppressions", "generation:5465:path-proof:operator"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(),
          'active', $2, '{}'::jsonb)`,
			scope.scopeID,
			scope.generationID,
		); err != nil {
			t.Fatalf("insert scope %s: %v", scope.scopeID, err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert generation %s: %v", scope.generationID, err)
		}
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:source:' || lpad(n::text, 6, '0'),
  'scope:5465:path-proof',
  'generation:5465:path-proof',
  'reducer_supply_chain_impact_finding',
  'stable:source:' || lpad(n::text, 6, '0'),
  '1.0.0', 'synthetic', 1, 'observed', 'synthetic',
  'source:' || lpad(n::text, 6, '0'), NOW(), NOW(),
  jsonb_build_object(
    'finding_id', 'finding:' || lpad(((n + 1) / 2)::text, 6, '0'),
    'cve_id', 'CVE-2026-' || lpad(((n + 1) / 2)::text, 5, '0'),
    'package_id', 'pkg:npm/example-' || lpad(((n + 1) / 2)::text, 5, '0') || '@1.0.0',
    'repository_id', 'repository:r_' || lpad(((n + 1) / 2)::text, 5, '0'),
    'impact_status', 'affected_exact',
    'detection_profile', 'precise',
    'observed_version', '1.0.0',
    'match_reason', 'npm_semver_affected_range',
    'cvss_score', 8.0,
    'priority_score', '50',
    'priority_bucket', 'high',
    'suppression_state', 'active',
    'suppression', jsonb_build_object('state', 'active'),
    'service_ids', '[]'::jsonb,
    'workload_ids', '[]'::jsonb,
    'environments', '[]'::jsonb,
    'evidence_fact_ids', '[]'::jsonb
  )
FROM generate_series(1, $1::integer) AS n`,
		suppressionPathsSourceRows,
	); err != nil {
		t.Fatalf("seed source findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  'fact:operator:' || lpad(n::text, 6, '0'),
  'operator:vulnerability_suppressions',
  'generation:5465:path-proof:operator',
  'reducer_supply_chain_impact_finding',
  'stable:operator:' || lpad(n::text, 6, '0'),
  '1.0.0', 'synthetic', 1, 'operator', 'eshu_policy',
  'operator:' || lpad(n::text, 6, '0'), NOW(), NOW(),
  jsonb_build_object(
    'finding_id', 'finding:' || lpad(n::text, 6, '0'),
    'cve_id', 'CVE-2026-' || lpad(n::text, 5, '0'),
    'package_id', 'pkg:npm/example-' || lpad(n::text, 5, '0') || '@1.0.0',
    'repository_id', 'repository:r_' || lpad(n::text, 5, '0'),
    'impact_status', 'affected_exact',
    'detection_profile', 'precise',
    'observed_version', '1.0.0',
    'match_reason', 'npm_semver_affected_range',
    'cvss_score', 8.0,
    'priority_score', '50',
    'priority_bucket', 'high',
    'suppression_state', 'ignored',
    'suppression', jsonb_build_object(
      'state', 'ignored',
      'suppression_id', 'suppression:' || lpad(n::text, 6, '0'),
      'expires_at', '2099-01-01T00:00:00Z',
      'reason', 'synthetic performance proof'
    ),
    'service_ids', '[]'::jsonb,
    'workload_ids', '[]'::jsonb,
    'environments', '[]'::jsonb,
    'evidence_fact_ids', '[]'::jsonb
  )
FROM generate_series(1, $1::integer) AS n`,
		suppressionPathsOperatorRows,
	); err != nil {
		t.Fatalf("seed operator findings: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;`); err != nil {
		t.Fatalf("analyze suppression path facts: %v", err)
	}
}

func measureSuppressionPath(t *testing.T, run func() error) []float64 {
	t.Helper()
	// Two runs: wall time is reported, never gated (#7553).
	values := make([]float64, 2)
	for i := range values {
		started := time.Now()
		if err := run(); err != nil {
			t.Fatalf("measure suppression path run %d: %v", i, err)
		}
		values[i] = float64(time.Since(started).Microseconds()) / 1_000
	}
	return values
}

func suppressionPathMedian(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return (sorted[len(sorted)/2-1] + sorted[len(sorted)/2]) / 2
}

func suppressionPathP95(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := (len(sorted)*95+99)/100 - 1
	return sorted[index]
}
