// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

const (
	suppressionSQLProofRows     = 100_000
	suppressionSQLProofMatches  = 250
	suppressionSQLProofAdvisory = "GHSA-2026-proof-advisory"
	suppressionSQLProofCVE      = "CVE-2026-54650"
)

type suppressionSQLPlan struct {
	ExecutionTime float64 `json:"Execution Time"`
	PlanningTime  float64 `json:"Planning Time"`
	Plan          struct {
		ActualRows       float64 `json:"Actual Rows"`
		SharedHitBlocks  int64   `json:"Shared Hit Blocks"`
		SharedReadBlocks int64   `json:"Shared Read Blocks"`
	} `json:"Plan"`
}

func TestSupplyChainImpactAdvisoryFilterPlanLive(t *testing.T) {
	ctx, db := openSuppressionSQLProofDB(t)
	applySuppressionSQLProofDefinitions(t, ctx, db)
	seedSuppressionSQLProofFacts(t, ctx, db)

	legacyQuery := suppressionSQLProofLegacyQuery(t)
	legacyArgs := suppressionSQLProofLegacyArgs([]string{suppressionSQLProofCVE})
	currentLegacyArgs := suppressionSQLProofCurrentArgs(
		[]string{suppressionSQLProofCVE},
		nil,
	)
	advisoryArgs := suppressionSQLProofCurrentArgs(
		nil,
		[]string{suppressionSQLProofAdvisory},
	)

	legacyIDs := suppressionSQLProofFactIDs(t, ctx, db, legacyQuery, legacyArgs)
	currentLegacyIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentLegacyArgs,
	)
	if strings.Join(legacyIDs, "\n") != strings.Join(currentLegacyIDs, "\n") {
		t.Fatal("current query changed the legacy CVE result set")
	}
	if len(legacyIDs) != suppressionSQLProofMatches {
		t.Fatalf("legacy CVE rows = %d, want %d", len(legacyIDs), suppressionSQLProofMatches)
	}

	advisoryIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		advisoryArgs,
	)
	if len(advisoryIDs) != suppressionSQLProofMatches {
		t.Fatalf("advisory-only rows = %d, want %d", len(advisoryIDs), suppressionSQLProofMatches)
	}
	for _, factID := range advisoryIDs {
		if !strings.HasPrefix(factID, "fact:advisory:") {
			t.Fatalf("advisory-only query returned unintended fact %q", factID)
		}
	}

	legacyPlans := suppressionSQLProofPlans(t, ctx, db, legacyQuery, legacyArgs)
	currentLegacyPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentLegacyArgs,
	)
	advisoryPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		advisoryArgs,
	)
	legacyMedian := suppressionSQLProofMedian(legacyPlans)
	currentMedian := suppressionSQLProofMedian(currentLegacyPlans)
	advisoryMedian := suppressionSQLProofMedian(advisoryPlans)
	legacyP95 := suppressionSQLProofP95(legacyPlans)
	currentP95 := suppressionSQLProofP95(currentLegacyPlans)
	if currentMedian > legacyMedian*1.10 {
		t.Fatalf(
			"legacy filter median regressed by more than 10%%: old=%.3fms current=%.3fms",
			legacyMedian,
			currentMedian,
		)
	}
	if currentP95 > legacyP95*1.10 {
		t.Fatalf(
			"legacy filter p95 regressed by more than 10%%: old=%.3fms current=%.3fms",
			legacyP95,
			currentP95,
		)
	}
	lastAdvisory := advisoryPlans[len(advisoryPlans)-1]
	if int(lastAdvisory.Plan.ActualRows) != suppressionSQLProofMatches {
		t.Fatalf(
			"advisory plan actual rows = %.0f, want %d",
			lastAdvisory.Plan.ActualRows,
			suppressionSQLProofMatches,
		)
	}
	t.Logf(
		"100k active facts; 10 warm measurements: legacy old median=%.3fms p95=%.3fms, "+
			"legacy current median=%.3fms p95=%.3fms, advisory-only median=%.3fms p95=%.3fms; "+
			"advisory rows=%d shared_hit_blocks=%d shared_read_blocks=%d",
		legacyMedian,
		legacyP95,
		currentMedian,
		currentP95,
		advisoryMedian,
		suppressionSQLProofP95(advisoryPlans),
		int(lastAdvisory.Plan.ActualRows),
		lastAdvisory.Plan.SharedHitBlocks,
		lastAdvisory.Plan.SharedReadBlocks,
	)
}

func openSuppressionSQLProofDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5465 SQL proofs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	schemaName := fmt.Sprintf("suppression_sql_5465_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schemaName)); err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA "+pq.QuoteIdentifier(schemaName)+" CASCADE",
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

func applySuppressionSQLProofDefinitions(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	required := map[string]bool{
		"ingestion_scopes":                      true,
		"scope_generations":                     true,
		"fact_records":                          true,
		"supply_chain_impact_canonical_winners": true,
	}
	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if required[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(required) {
		t.Fatalf("isolated schema definitions = %d, want %d", len(definitions), len(required))
	}
	if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
		t.Fatalf("apply isolated Postgres schema: %v", err)
	}
}

func seedSuppressionSQLProofFacts(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, active_generation_id, payload
) VALUES (
  'scope:5465:sql-proof', 'vulnerability_intelligence', 'synthetic',
  'scope:5465:sql-proof', 'synthetic', 'scope:5465:sql-proof',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z', 'active',
  'generation:5465:sql-proof', '{}'::jsonb
);
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES (
  'generation:5465:sql-proof', 'scope:5465:sql-proof', 'synthetic',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z',
  'active', '2026-07-27T12:00:00Z', '{}'::jsonb
)`); err != nil {
		t.Fatalf("seed active supply-chain scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  schema_version, collector_kind, fencing_token, source_confidence,
  source_system, source_fact_key, observed_at, ingested_at, payload
)
SELECT
  CASE
    WHEN n <= 250 THEN format('fact:advisory:%06s', n)
    WHEN n <= 500 THEN format('fact:cve:%06s', n)
    ELSE format('fact:other:%06s', n)
  END,
  'scope:5465:sql-proof', 'generation:5465:sql-proof', 'vulnerability.cve',
  format('stable:%06s', n), '1.0.0', 'synthetic', 1, 'reported',
  'synthetic', format('source:%06s', n),
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z',
  CASE
    WHEN n <= 125 THEN jsonb_build_object('advisory_id', $1::text)
    WHEN n <= 250 THEN jsonb_build_object('scope', jsonb_build_object('advisory_id', $1::text))
    WHEN n <= 375 THEN jsonb_build_object('cve_id', $2::text)
    WHEN n <= 500 THEN jsonb_build_object('scope', jsonb_build_object('cve_id', $2::text))
    ELSE jsonb_build_object('advisory_id', format('GHSA-2026-other-%06s', n))
  END
FROM generate_series(1, $3::integer) AS n;
`,
		suppressionSQLProofAdvisory,
		suppressionSQLProofCVE,
		suppressionSQLProofRows,
	); err != nil {
		t.Fatalf("seed 100k active supply-chain facts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
ANALYZE fact_records;
ANALYZE ingestion_scopes;
ANALYZE scope_generations;`); err != nil {
		t.Fatalf("analyze active supply-chain facts: %v", err)
	}
}

func suppressionSQLProofLegacyQuery(t *testing.T) string {
	t.Helper()
	query := strings.Replace(
		listActiveSupplyChainImpactFactsQuery,
		"      OR (\n"+
			"          cardinality($4::text[]) > 0\n"+
			"          AND (\n"+
			"              fact.payload->>'advisory_id' = ANY($4::text[])\n"+
			"              OR fact.payload->'scope'->>'advisory_id' = ANY($4::text[])\n"+
			"          )\n"+
			"      )\n",
		"",
		1,
	)
	for parameter := 5; parameter <= 12; parameter++ {
		query = strings.ReplaceAll(
			query,
			"$"+strconv.Itoa(parameter),
			"__SUPPRESSION_PROOF_ARG_"+strconv.Itoa(parameter)+"__",
		)
	}
	for parameter := 5; parameter <= 12; parameter++ {
		query = strings.ReplaceAll(
			query,
			"__SUPPRESSION_PROOF_ARG_"+strconv.Itoa(parameter)+"__",
			"$"+strconv.Itoa(parameter-1),
		)
	}
	if strings.Contains(query, "advisory_id' = ANY") {
		t.Fatal("legacy query still contains advisory filter")
	}
	return query
}

func suppressionSQLProofCurrentArgs(cveIDs, advisoryIDs []string) []any {
	empty := []string{}
	return []any{
		empty, empty, cveIDs, advisoryIDs, empty, empty,
		empty, empty, empty, empty, "", 1_000,
	}
}

func suppressionSQLProofLegacyArgs(cveIDs []string) []any {
	empty := []string{}
	return []any{
		empty, empty, cveIDs, empty, empty, empty,
		empty, empty, empty, "", 1_000,
	}
}

func suppressionSQLProofFactIDs(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args []any,
) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "SELECT fact_id FROM ("+query+") AS selected", args...)
	if err != nil {
		t.Fatalf("query fact IDs: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan fact ID: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate fact IDs: %v", err)
	}
	return ids
}

func suppressionSQLProofPlans(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args []any,
) []suppressionSQLPlan {
	t.Helper()
	plans := make([]suppressionSQLPlan, 0, 10)
	for run := 0; run < 11; run++ {
		var raw []byte
		if err := db.QueryRowContext(
			ctx,
			"EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query,
			args...,
		).Scan(&raw); err != nil {
			t.Fatalf("EXPLAIN run %d: %v", run, err)
		}
		var decoded []suppressionSQLPlan
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode EXPLAIN run %d: %v", run, err)
		}
		if len(decoded) != 1 {
			t.Fatalf("EXPLAIN run %d plans = %d, want 1", run, len(decoded))
		}
		if run == 0 {
			continue
		}
		if decoded[0].Plan.SharedReadBlocks != 0 {
			t.Fatalf(
				"EXPLAIN run %d read %d shared blocks after warmup",
				run,
				decoded[0].Plan.SharedReadBlocks,
			)
		}
		plans = append(plans, decoded[0])
	}
	return plans
}

func suppressionSQLProofMedian(plans []suppressionSQLPlan) float64 {
	values := make([]float64, len(plans))
	for i := range plans {
		values[i] = plans[i].ExecutionTime
	}
	sort.Float64s(values)
	return (values[len(values)/2-1] + values[len(values)/2]) / 2
}

func suppressionSQLProofP95(plans []suppressionSQLPlan) float64 {
	values := make([]float64, len(plans))
	for i := range plans {
		values[i] = plans[i].ExecutionTime
	}
	sort.Float64s(values)
	return values[len(values)-1]
}
