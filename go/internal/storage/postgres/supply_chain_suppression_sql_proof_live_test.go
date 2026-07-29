// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
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
	suppressionSQLProofBaseSHA  = "37649ca04ad51408494af96955876a8c8ad49f22"
	suppressionSQLProofBaseHash = "2bed8a571c86702c5090d305fa4dfa31889b6c54208d9cf00fb81f4447490bf3"
)

// suppressionSQLProofBaseQuery is byte-for-byte the query constant from
// facts_active_supply_chain_impact.go at suppressionSQLProofBaseSHA.
//
//go:embed testdata/list_active_supply_chain_impact_facts_37649ca.sql
var suppressionSQLProofBaseQuery string

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

	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(suppressionSQLProofBaseQuery))); got != suppressionSQLProofBaseHash {
		t.Fatalf("exact-base query hash = %s, want %s", got, suppressionSQLProofBaseHash)
	}

	baseCVEArgs := suppressionSQLProofBaseArgs(
		[]string{suppressionSQLProofCVE},
		nil,
	)
	currentCVEArgs := suppressionSQLProofCurrentArgs(
		[]string{suppressionSQLProofCVE},
		nil,
	)
	baseAdvisoryArgs := suppressionSQLProofBaseArgs(
		nil,
		[]string{suppressionSQLProofAdvisory},
	)
	currentAdvisoryArgs := suppressionSQLProofCurrentArgs(
		nil,
		[]string{suppressionSQLProofAdvisory},
	)

	baseCVEIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		suppressionSQLProofBaseQuery,
		baseCVEArgs,
	)
	currentCVEIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentCVEArgs,
	)
	if strings.Join(baseCVEIDs, "\n") != strings.Join(currentCVEIDs, "\n") {
		t.Fatal("current query changed the exact-base CVE result set")
	}
	if len(baseCVEIDs) != suppressionSQLProofMatches {
		t.Fatalf("exact-base CVE rows = %d, want %d", len(baseCVEIDs), suppressionSQLProofMatches)
	}

	baseAdvisoryIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		suppressionSQLProofBaseQuery,
		baseAdvisoryArgs,
	)
	currentAdvisoryIDs := suppressionSQLProofFactIDs(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentAdvisoryArgs,
	)
	if got, want := len(baseAdvisoryIDs), suppressionSQLProofMatches/2; got != want {
		t.Fatalf("exact-base advisory rows = %d, want %d exact top-level matches", got, want)
	}
	if got, want := len(currentAdvisoryIDs), suppressionSQLProofMatches; got != want {
		t.Fatalf("current advisory rows = %d, want %d normalized matches", got, want)
	}
	if got, want := len(currentAdvisoryIDs)-len(baseAdvisoryIDs), suppressionSQLProofMatches/2; got != want {
		t.Fatalf("normalized advisory delta = %d, want %d nested matches", got, want)
	}
	for _, factID := range currentAdvisoryIDs {
		if !strings.HasPrefix(factID, "fact:advisory:") {
			t.Fatalf("advisory-only query returned unintended fact %q", factID)
		}
	}

	baseCVEPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		suppressionSQLProofBaseQuery,
		baseCVEArgs,
	)
	currentCVEPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentCVEArgs,
	)
	baseAdvisoryPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		suppressionSQLProofBaseQuery,
		baseAdvisoryArgs,
	)
	currentAdvisoryPlans := suppressionSQLProofPlans(
		t,
		ctx,
		db,
		listActiveSupplyChainImpactFactsQuery,
		currentAdvisoryArgs,
	)
	baseCVEMedian := suppressionSQLProofMedian(baseCVEPlans)
	currentCVEMedian := suppressionSQLProofMedian(currentCVEPlans)
	baseCVEP95 := suppressionSQLProofP95(baseCVEPlans)
	currentCVEP95 := suppressionSQLProofP95(currentCVEPlans)
	if currentCVEMedian > baseCVEMedian*1.10 {
		t.Fatalf(
			"equivalent CVE filter median regressed by more than 10%%: base=%.3fms current=%.3fms",
			baseCVEMedian,
			currentCVEMedian,
		)
	}
	if currentCVEP95 > baseCVEP95*1.10 {
		t.Fatalf(
			"equivalent CVE filter p95 regressed by more than 10%%: base=%.3fms current=%.3fms",
			baseCVEP95,
			currentCVEP95,
		)
	}
	lastAdvisory := currentAdvisoryPlans[len(currentAdvisoryPlans)-1]
	if int(lastAdvisory.Plan.ActualRows) != suppressionSQLProofMatches {
		t.Fatalf(
			"advisory plan actual rows = %.0f, want %d",
			lastAdvisory.Plan.ActualRows,
			suppressionSQLProofMatches,
		)
	}
	t.Logf(
		"100k active facts; exact base %s; 10 warm measurements: "+
			"CVE base median=%.3fms p95=%.3fms, CVE current median=%.3fms p95=%.3fms; "+
			"advisory base median=%.3fms p95=%.3fms rows=%d, "+
			"advisory current median=%.3fms p95=%.3fms rows=%d; "+
			"current advisory shared_hit_blocks=%d shared_read_blocks=%d",
		suppressionSQLProofBaseSHA,
		baseCVEMedian,
		baseCVEP95,
		currentCVEMedian,
		currentCVEP95,
		suppressionSQLProofMedian(baseAdvisoryPlans),
		suppressionSQLProofP95(baseAdvisoryPlans),
		len(baseAdvisoryIDs),
		suppressionSQLProofMedian(currentAdvisoryPlans),
		suppressionSQLProofP95(currentAdvisoryPlans),
		int(lastAdvisory.Plan.ActualRows),
		lastAdvisory.Plan.SharedHitBlocks,
		lastAdvisory.Plan.SharedReadBlocks,
	)
}

func openSuppressionSQLProofDB(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5466 SQL proofs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })

	schemaName := fmt.Sprintf("suppression_sql_5466_%d", time.Now().UnixNano())
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
  'scope:5466:sql-proof', 'vulnerability_intelligence', 'synthetic',
  'scope:5466:sql-proof', 'synthetic', 'scope:5466:sql-proof',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z', 'active',
  'generation:5466:sql-proof', '{}'::jsonb
);
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES (
  'generation:5466:sql-proof', 'scope:5466:sql-proof', 'synthetic',
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z',
  'active', '2026-07-27T12:00:00Z', '{}'::jsonb
)`); err != nil {
		t.Fatalf("seed active supply-chain scope: %v", err)
	}
	if _, err := db.ExecContext(
		ctx, `
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
  'scope:5466:sql-proof', 'generation:5466:sql-proof',
  -- rows 126-250 and 376-500 carry identity values nested under "scope",
  -- which only ever appears on a vulnerability.suppression fact in
  -- production (no other
  -- fact kind's schema has a "scope" sub-object -- see
  -- sdk/go/factschema/vulnerability/v1); the query's normalized
  -- scope-nested advisory_id predicate ($18) is
  -- gated to fact_kind = 'vulnerability.suppression' to match that reality.
  -- The same guard now applies to every nested suppression scope identity,
  -- avoiding Unicode normalization work on the 99.75% unrelated rows in
  -- this representative corpus.
  CASE
    WHEN (n > 125 AND n <= 250) OR (n > 375 AND n <= 500)
      THEN 'vulnerability.suppression'
    ELSE 'vulnerability.cve'
  END,
  format('stable:%06s', n), '1.0.0', 'synthetic', 1, 'reported',
  'synthetic', format('source:%06s', n),
  '2026-07-27T12:00:00Z', '2026-07-27T12:00:00Z',
  CASE
    WHEN n <= 125 THEN jsonb_build_object('advisory_id', $1::text)
    WHEN n <= 250 THEN jsonb_build_object(
      'scope',
      jsonb_build_object('advisory_id', chr(160) || lower($1::text) || chr(160))
    )
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

// suppressionSQLProofCurrentArgs binds against the current, full
// listActiveSupplyChainImpactFactsQuery (19 placeholders). cveIDs and
// advisoryIDs each feed BOTH their exact-match slot and their lower(btrim(...))
// normalized sibling slot, mirroring exactly how
// listActiveSupplyChainImpactFactsPage binds the same filter value twice in
// production. The final `false` is the compound keyset cursor's boolean
// half ($19, #5466 round-8 review F-3); its value is irrelevant here since
// $11 (the cursor's fact_id half) is empty, which bypasses the cursor
// predicate entirely (first page), but it must still be present and
// well-typed for Postgres to plan the query.
func suppressionSQLProofCurrentArgs(cveIDs, advisoryIDs []string) []any {
	empty := []string{}
	return []any{
		empty, empty, cveIDs, advisoryIDs, empty, empty,
		empty, empty, empty, empty, "", 1_000,
		empty, empty,
		lowerCleanedStringFilterValues(cveIDs),
		empty, empty,
		lowerCleanedStringFilterValues(advisoryIDs),
		false,
	}
}

// suppressionSQLProofBaseArgs binds the exact 12-placeholder query from
// suppressionSQLProofBaseSHA.
func suppressionSQLProofBaseArgs(cveIDs, advisoryIDs []string) []any {
	empty := []string{}
	return []any{
		empty, empty, cveIDs, advisoryIDs, empty, empty,
		empty, empty, empty, empty, "", 1_000,
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
