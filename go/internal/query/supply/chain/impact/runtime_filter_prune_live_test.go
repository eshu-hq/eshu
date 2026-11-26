// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11493 pruned-vs-shipped multiset differential for the literal-empty environment dimension: the only live proof that the stub CTE is behavior-identical runs on Postgres with seeded padded, overlap, tombstone and inactive-generation rows.
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// TestSupplyChainImpactRuntimeFilterPruningIsBehaviorIdenticalLive is the
// failing-first differential for eshu-hq/eshu#11493 (arbiter ruling
// 7178944958, decision 7.ii): the shipped full-scan text and the pruned
// stub text, both derived from the shipped builder output and never
// hand-copied, must return identical multisets (both-direction EXCEPT ALL,
// duplicates counted) over a corpus covering exact/padded workload_id,
// exact/padded entity_keys in array and string forms, the overlap row,
// absent entity_keys, absent workload_id, non-workload: parameters, empty
// parameters, combined service+workload filters, empty and non-empty grant
// arrays, tombstones, and inactive generations. It runs under
// force_custom_plan and force_generic_plan, and logs per-variant
// EXPLAIN (ANALYZE, BUFFERS) (decision 7.iv).
func TestSupplyChainImpactRuntimeFilterPruningIsBehaviorIdenticalLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #11493 pruning differential")
	}

	// The differential's pruned input must track production: the exact CTE
	// text the explain entry points embed is a substring of the shipped
	// ExplainFindingQuery.
	productionCTE := supplyChainImpactRuntimeFilterCTE("$9", "$8", "''", "$11", "$12")
	if !strings.Contains(ExplainFindingQuery, productionCTE) {
		t.Fatal("pruned filter text drifted from the production explain query: the differential would prove the wrong text")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db}); err != nil {
		t.Fatalf("apply production Postgres schema: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	seedRuntimeFilterPruneCorpus(t, ctx, tx)

	// Both texts derive from the shipped builder output: the shipped text is
	// the pre-pruning rendering of the template, the pruned text is the
	// builder output. The environment dimension is the literal empty in
	// both, so $3/$4 are the repository and scope grants.
	shippedCTE := sprintfRuntimeFilterCTE("$1", "$2", "''", "$3", "$4")
	prunedCTE := supplyChainImpactRuntimeFilterCTE("$1", "$2", "''", "$3", "$4")
	shippedStmt := "WITH " + shippedCTE + " SELECT filter_kind, repository_id FROM runtime_filter_repositories"
	prunedStmt := "WITH " + prunedCTE + " SELECT filter_kind, repository_id FROM runtime_filter_repositories"

	if _, err := tx.ExecContext(ctx, `PREPARE shipped_prune_probe(text, text, text[], text[]) AS `+shippedStmt); err != nil {
		t.Fatalf("prepare shipped probe: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `PREPARE pruned_prune_probe(text, text, text[], text[]) AS `+prunedStmt); err != nil {
		t.Fatalf("prepare pruned probe: %v", err)
	}

	// Named prepared statements so the forced plan_cache_mode applies to
	// the executions below; database/sql Prepare takes the extended
	// protocol path, which binds arguments to the server-side statement
	// correctly (EXECUTE with $N arguments does not).
	shippedProbe, err := tx.PrepareContext(ctx, shippedStmt)
	if err != nil {
		t.Fatalf("prepare shipped probe: %v", err)
	}
	defer func() { _ = shippedProbe.Close() }()
	prunedProbe, err := tx.PrepareContext(ctx, prunedStmt)
	if err != nil {
		t.Fatalf("prepare pruned probe: %v", err)
	}
	defer func() { _ = prunedProbe.Close() }()

	for _, planMode := range []string{"force_custom_plan", "force_generic_plan"} {
		if _, err := tx.ExecContext(ctx, `SET LOCAL plan_cache_mode = `+planMode); err != nil {
			t.Fatalf("set plan_cache_mode %s: %v", planMode, err)
		}
		for _, tc := range pruneDifferentialCases() {
			t.Run(planMode+"/"+tc.name, func(t *testing.T) {
				shipped := queryPruneProbe(t, ctx, shippedProbe, "shipped", tc)
				pruned := queryPruneProbe(t, ctx, prunedProbe, "pruned", tc)
				if !reflect.DeepEqual(shipped, pruned) {
					t.Fatalf("multiset difference (shipped=%v pruned=%v)", shipped, pruned)
				}
				if tc.want != nil && !reflect.DeepEqual(shipped, tc.want) {
					t.Fatalf("absolute answer drifted (got=%v want=%v)", shipped, tc.want)
				}
				t.Logf("case %s rows=%d equal", tc.name, len(shipped))
			})
		}
		logPruneProbePlan(t, ctx, tx, "shipped", "shipped_prune_probe", planMode)
		logPruneProbePlan(t, ctx, tx, "pruned", "pruned_prune_probe", planMode)
	}
}

// pruneDifferentialCase is one seeded-corpus probe of the filter CTE pair.
// want pins the absolute answer of the cases that discriminate the
// forbidden rewrites: the overlap row must come back once (guard removal
// would double-emit it under UNION ALL), and the absent-entity-keys row
// must come back once (plain NOT would exclude it through the NULL arm-1
// OR). A differential alone would stay green if both texts broke the same
// way; these pins keep it honest.
type pruneDifferentialCase struct {
	name     string
	service  string
	workload string
	repos    []string
	scopes   []string
	want     []string
}

// pruneDifferentialCases covers the ruling's seed matrix: exact and padded
// workload scalars, entity_keys in array and string forms, the overlap row,
// absent keys, non-workload: parameters, empty parameters, combined
// filters, and empty/non-empty grant arrays. Tombstone, inactive
// generation, and environment rows are in the corpus and must never
// surface.
func pruneDifferentialCases() []pruneDifferentialCase {
	return []pruneDifferentialCase{
		{name: "empty_parameters", service: "", workload: ""},
		{name: "exact_workload_scalar", workload: "workload:11493:exact", want: []string{"workload|repository:11493:main"}},
		{name: "padded_workload_scalar", workload: "workload:11493:padded"},
		{name: "entity_array_exact", workload: "workload:11493:entity-exact"},
		{name: "entity_array_padded", workload: "workload:11493:entity-padded"},
		{name: "entity_string_exact", workload: "workload:11493:entity-string"},
		{name: "entity_string_padded", workload: "workload:11493:entity-string-padded"},
		{name: "overlap_row", workload: "workload:11493:overlap", want: []string{"workload|repository:11493:main"}},
		{name: "absent_entity_keys", workload: "workload:11493:no-entity", want: []string{"workload|repository:11493:main"}},
		{name: "absent_workload_scalar", workload: "workload:11493:no-scalar"},
		{name: "non_workload_parameter", workload: "service:11493:not-a-workload"},
		{name: "exact_service", service: "service:11493:exact"},
		{name: "padded_service", service: "service:11493:padded"},
		{name: "combined_service_workload", service: "service:11493:combo", workload: "workload:11493:combo"},
		{name: "combined_mismatch", service: "service:11493:combo", workload: "workload:11493:exact"},
		{name: "repo_grant_match", workload: "workload:11493:exact", repos: []string{"repository:11493:main"}},
		{name: "scope_grant_match", workload: "workload:11493:exact", scopes: []string{"scope:11493:a"}},
		{name: "grants_exclude", workload: "workload:11493:exact", repos: []string{"repository:11493:other"}, scopes: []string{"scope:11493:b"}},
		{name: "scope_b_row", workload: "workload:11493:scope-b", scopes: []string{"scope:11493:b"}},
	}
}

// queryPruneProbe executes one prepared probe and returns the sorted
// filter_kind/repository_id multiset, so duplicates count and order does
// not. Sorted-slice equality is multiset equality: the Go equivalent of a
// both-direction EXCEPT ALL over the UNION ALL candidate legs.
func queryPruneProbe(t *testing.T, ctx context.Context, probe *sql.Stmt, variant string, tc pruneDifferentialCase) []string {
	t.Helper()
	rows, err := probe.QueryContext(ctx,
		tc.service, tc.workload, array.Of(tc.repos), array.Of(tc.scopes))
	if err != nil {
		t.Fatalf("execute %s %s: %v", variant, tc.name, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var kind, repo string
		if err := rows.Scan(&kind, &repo); err != nil {
			t.Fatalf("scan %s %s: %v", variant, tc.name, err)
		}
		out = append(out, kind+"|"+repo)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %s %s: %v", variant, tc.name, err)
	}
	sort.Strings(out)
	return out
}

// logPruneProbePlan records EXPLAIN (ANALYZE, BUFFERS) for one probe
// variant: the per-arm plan proof of decision 7.iv. Wall time is logged,
// never gated. EXECUTE carries inline literals because EXECUTE does not
// take $N arguments over this driver path.
func logPruneProbePlan(t *testing.T, ctx context.Context, tx *sql.Tx, variant, probe, planMode string) {
	t.Helper()
	var raw json.RawMessage
	seed := pruneDifferentialCases()[2]
	execute := `EXECUTE ` + probe + `(` +
		sqlQuoteLiteral(seed.service) + `, ` +
		sqlQuoteLiteral(seed.workload) + `, ` +
		sqlQuoteTextArray(seed.repos) + `, ` +
		sqlQuoteTextArray(seed.scopes) + `)`
	if err := tx.QueryRowContext(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+execute).Scan(&raw); err != nil {
		t.Fatalf("explain %s %s: %v", variant, planMode, err)
	}
	plan := string(raw)
	planning, execution := pruneProbePlanTimings(t, raw)
	t.Logf("plan %s %s bytes=%d planning=%.3fms execution=%.3fms", variant, planMode, len(plan), planning, execution)
	// Both variants keep the environment CTE wired into the candidates:
	// the shipped one collapses its two guarded legs to one-time
	// filters, the pruned one is the literal-false-comparison stub. The differential
	// above proves the rows identical; the shapes below pin the
	// before/after the ruling asks to record.
	if !strings.Contains(plan, "runtime_environment_matches") {
		t.Fatalf("%s plan lost the environment CTE under %s", variant, planMode)
	}
	if variant == "shipped" && !strings.Contains(plan, "One-Time Filter") {
		t.Fatalf("shipped plan no longer prunes the empty environment legs at plan time under %s", planMode)
	}
}

// pruneProbePlanTimings reads the planning and execution milliseconds out
// of an EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) document.
func pruneProbePlanTimings(t *testing.T, raw json.RawMessage) (planning, execution float64) {
	t.Helper()
	var docs []struct {
		PlanningTime  float64 `json:"Planning Time"`
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &docs); err != nil || len(docs) != 1 {
		t.Fatalf("decode plan timings: %v", err)
	}
	return docs[0].PlanningTime, docs[0].ExecutionTime
}

// sqlQuoteLiteral renders a SQL string literal.
func sqlQuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// sqlQuoteTextArray renders a text[] literal. Every element is
// double-quoted so repository and scope ids need no per-value audit.
func sqlQuoteTextArray(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ReplaceAll(item, `\`, `\\`)
		quoted = append(quoted, `"`+strings.ReplaceAll(item, `"`, `\"`)+`"`)
	}
	return "'{" + strings.Join(quoted, ",") + "}'"
}

// seedRuntimeFilterPruneCorpus writes the ruling's seed matrix into the
// proof transaction: exact and padded scalars, array and string entity
// keys, the overlap row (padded workload_id plus an exact entity_keys
// entry), rows with absent keys, a tombstone, a superseded-generation row,
// environment rows the pruned dimension must ignore, and a second scope
// for grant tests.
func seedRuntimeFilterPruneCorpus(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	scopes := []struct {
		scopeID      string
		generationID string
	}{
		{scopeID: "scope:11493:a", generationID: "generation:11493:a:active"},
		{scopeID: "scope:11493:b", generationID: "generation:11493:b:active"},
	}
	for _, scope := range scopes {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'synthetic', 'synthetic', $1, 'synthetic', $1, NOW(), NOW(), 'active', '{}'::jsonb)`,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert scope %s: %v", scope.scopeID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', NOW(), NOW(), 'active', NOW(), '{}'::jsonb)`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("insert generation %s: %v", scope.generationID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
			scope.generationID,
			scope.scopeID,
		); err != nil {
			t.Fatalf("activate generation %s: %v", scope.generationID, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, superseded_at, payload
) VALUES ('generation:11493:a:stale', 'scope:11493:a', 'synthetic', NOW(), NOW(), 'superseded', NOW(), '{}'::jsonb)`); err != nil {
		t.Fatalf("insert stale generation: %v", err)
	}

	insert := func(factID, scopeID, generationID, factKind string, tombstone bool, payload map[string]any) {
		t.Helper()
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal fact %s: %v", factID, err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
  source_system, source_fact_key, observed_at, ingested_at,
  is_tombstone, payload
) VALUES ($1, $2, $3, $4, $1, 'synthetic', $1, NOW(), NOW(), $5, $6::jsonb)`,
			factID, scopeID, generationID, factKind, tombstone, string(payloadJSON),
		); err != nil {
			t.Fatalf("insert fact %s: %v", factID, err)
		}
	}
	const (
		workloadKind = "reducer_workload_identity"
		serviceKind  = "reducer_service_catalog_correlation"
		envKind      = "reducer_ci_cd_run_correlation"
		scopeA       = "scope:11493:a"
		genA         = "generation:11493:a:active"
		scopeB       = "scope:11493:b"
		genB         = "generation:11493:b:active"
		repoMain     = "repository:11493:main"
	)
	workload := func(factID string, payload map[string]any) {
		t.Helper()
		payload["repository_id"] = repoMain
		insert(factID, scopeA, genA, workloadKind, false, payload)
	}
	workload("fact:11493:workload:exact", map[string]any{"workload_id": "workload:11493:exact"})
	workload("fact:11493:workload:padded", map[string]any{"workload_id": "  workload:11493:padded  "})
	workload("fact:11493:entity:array-exact", map[string]any{"entity_keys": []string{"workload:11493:entity-exact"}})
	workload("fact:11493:entity:array-padded", map[string]any{"entity_keys": []string{"  workload:11493:entity-padded  "}})
	workload("fact:11493:entity:string-exact", map[string]any{"entity_keys": "workload:11493:entity-string"})
	workload("fact:11493:entity:string-padded", map[string]any{"entity_keys": "  workload:11493:entity-string-padded  "})
	workload("fact:11493:workload:overlap", map[string]any{
		"workload_id": "  workload:11493:overlap  ",
		"entity_keys": []string{"workload:11493:overlap"},
	})
	workload("fact:11493:workload:no-entity", map[string]any{"workload_id": "workload:11493:no-entity"})
	workload("fact:11493:workload:no-scalar", map[string]any{"entity_keys": []string{"workload:11493:no-scalar"}})
	workload("fact:11493:workload:combo", map[string]any{"workload_id": "workload:11493:combo"})
	insert("fact:11493:workload:stale", scopeA, "generation:11493:a:stale", workloadKind, false,
		map[string]any{"repository_id": repoMain, "workload_id": "workload:11493:exact"})
	insert("fact:11493:workload:tombstone", scopeA, genA, workloadKind, true,
		map[string]any{"repository_id": repoMain, "workload_id": "workload:11493:exact"})
	insert("fact:11493:workload:scope-b", scopeB, genB, workloadKind, false,
		map[string]any{"repository_id": repoMain, "workload_id": "workload:11493:scope-b"})

	service := func(factID, serviceID string) {
		t.Helper()
		insert(factID, scopeA, genA, serviceKind, false, map[string]any{
			"repository_id": repoMain,
			"service_id":    serviceID,
			"outcome":       "exact",
		})
	}
	service("fact:11493:service:exact", "service:11493:exact")
	service("fact:11493:service:padded", "  service:11493:padded  ")
	service("fact:11493:service:combo", "service:11493:combo")

	insert("fact:11493:environment:ignored", scopeA, genA, envKind, false, map[string]any{
		"repository_id": repoMain,
		"environment":   "production-11493",
		"outcome":       "exact",
	})

	if _, err := tx.ExecContext(ctx, `ANALYZE fact_records`); err != nil {
		t.Fatalf("analyze seeded facts: %v", err)
	}
}
