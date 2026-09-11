// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	suppressionPathsSourceRows   = 100_000
	suppressionPathsCanonical    = 50_000
	suppressionPathsOperatorRows = 500
)

type suppressionPathMeasurement struct {
	name   string
	values []float64
}

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

	direct := NewPostgresSupplyChainImpactFindingStore(db)
	materialized := NewPostgresSupplyChainImpactFindingStoreWithReadModel(db, true)
	aggregates := NewPostgresSupplyChainImpactAggregateStore(db)
	listFilter := SupplyChainImpactFindingFilter{
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

	count, err := aggregates.CountSupplyChainImpactFindings(
		ctx,
		SupplyChainImpactAggregateFilter{
			DetectionProfile:  "precise",
			IncludeSuppressed: true,
		},
	)
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
		SupplyChainImpactExplanationFilter{FindingID: "finding:000501"},
	)
	if err != nil {
		t.Fatalf("explain finding: %v", err)
	}
	if explanation.Finding.FindingID != "finding:000501" {
		t.Fatalf("explain finding_id = %q, want finding:000501", explanation.Finding.FindingID)
	}

	measurements := []suppressionPathMeasurement{
		{
			name: "direct_list",
			values: measureSuppressionPath(t, func() error {
				_, err := direct.ListSupplyChainImpactFindings(ctx, listFilter)
				return err
			}),
		},
		{
			name: "materialized_list",
			values: measureSuppressionPath(t, func() error {
				_, err := materialized.ListSupplyChainImpactFindings(ctx, listFilter)
				return err
			}),
		},
		{
			name: "aggregate_count_and_facets",
			values: measureSuppressionPath(t, func() error {
				_, err := aggregates.CountSupplyChainImpactFindings(
					ctx,
					SupplyChainImpactAggregateFilter{
						DetectionProfile:  "precise",
						IncludeSuppressed: true,
					},
				)
				return err
			}),
		},
		{
			name: "explain",
			values: measureSuppressionPath(t, func() error {
				_, err := direct.ExplainSupplyChainImpact(
					ctx,
					SupplyChainImpactExplanationFilter{
						FindingID: "finding:000501",
					},
				)
				return err
			}),
		},
		{
			name: "winner_rebuild",
			values: measureSuppressionPath(t, func() error {
				return winners.RebuildAllWinners(ctx, time.Now().UTC())
			}),
		},
	}
	for _, measurement := range measurements {
		baseline := map[string][2]float64{
			"direct_list":                {177.170, 184.338},
			"materialized_list":          {60.163, 61.190},
			"aggregate_count_and_facets": {465.099, 468.035},
			"explain":                    {63.906, 65.402},
			"winner_rebuild":             {912.147, 943.861},
		}[measurement.name]
		median := suppressionPathMedian(measurement.values)
		p95 := suppressionPathP95(measurement.values)
		if median > baseline[0]*1.10 || p95 > baseline[1]*1.10 {
			t.Fatalf(
				"%s exceeded same-data origin/main 10%% ceiling: "+
					"median=%.3fms baseline=%.3fms p95=%.3fms baseline_p95=%.3fms",
				measurement.name,
				median,
				baseline[0],
				p95,
				baseline[1],
			)
		}
		t.Logf(
			"%s 100k-source/50k-canonical/500-operator median=%.3fms p95=%.3fms "+
				"origin_main_median=%.3fms origin_main_p95=%.3fms",
			measurement.name,
			median,
			p95,
			baseline[0],
			baseline[1],
		)
	}

	readAt := time.Now().UTC()
	listArgs := suppressionListPlanArgs(listFilter, readAt)
	aggregateFilter := SupplyChainImpactAggregateFilter{
		DetectionProfile:  "precise",
		IncludeSuppressed: true,
	}
	aggregateArgs := suppressionAggregatePlanArgs(aggregateFilter, readAt)
	plans := map[string]suppressionQueryPlanProof{
		"direct_list": explainSuppressionQueryPlan(
			t, ctx, db, ListSupplyChainImpactFindingsQuery, listArgs...,
		),
		"materialized_list": explainSuppressionQueryPlan(
			t, ctx, db, ListSupplyChainImpactFindingsFromWinnersQuery, listArgs...,
		),
		"aggregate_count": explainSuppressionQueryPlan(
			t, ctx, db, SupplyChainImpactAggregateCountQuery, aggregateArgs...,
		),
		"aggregate_priority_facet": explainSuppressionQueryPlan(
			t, ctx, db, SupplyChainImpactAggregatePriorityCountQuery, aggregateArgs...,
		),
		"explain": explainSuppressionQueryPlan(
			t, ctx, db, ExplainSupplyChainImpactFindingByPublicIDQuery,
			suppressionExplainPlanArgs(readAt)...,
		),
	}
	for name, plan := range plans {
		t.Logf(
			"%s plan node=%s execution=%.3fms planning=%.3fms rows=%.0f "+
				"shared_hit=%d shared_read=%d temp_read=%d temp_written=%d",
			name,
			plan.Plan.NodeType,
			plan.ExecutionTime,
			plan.PlanningTime,
			plan.Plan.ActualRows,
			plan.Plan.SharedHitBlocks,
			plan.Plan.SharedReadBlocks,
			plan.Plan.TempReadBlocks,
			plan.Plan.TempWrittenBlocks,
		)
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
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+pgarray.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+pgarray.QuoteIdentifier(schemaName)+" CASCADE",
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
	if err := run(); err != nil {
		t.Fatalf("warm suppression path: %v", err)
	}
	values := make([]float64, 20)
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
