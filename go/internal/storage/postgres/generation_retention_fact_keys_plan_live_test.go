// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7420 disposable PostgreSQL proof that every key sidecar delete prepares against the migrated schema and plans on its (scope_id, generation_id, fact_id) partition index under no statistics, ANALYZE, custom and forced-generic plans, with executed buffers bounded by rows deleted and seeded REDs for the fact_id-only form and a dropped index, enrolled in the live-postgres-readiness runner; bridges ESHU_GENERATION_RETENTION_PROOF_DSN. local run passed both tests in 6.93s of test time on PostgreSQL 18.6 on 2026-10-15
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	factkeys "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/keys"
)

// factKeyPlanCorpus seeds factKeyPlanScopes scopes with factKeyPlanGenerations
// generations of factKeyPlanFacts facts each, md5 fact ids like production
// sha ids, with a relationship key on every fact, a manifest key on a fifth
// and a registry key on a twentieth (the #7420 fixture shares).
const (
	factKeyPlanScopes      = 6
	factKeyPlanGenerations = 4
	factKeyPlanFacts       = 1500
	factKeyPlanPage        = 1000
)

// TestGenerationRetentionFactKeyStatementsPrepareLive fails when a #7420 key
// sidecar delete names a relation or column the migrations do not create.
func TestGenerationRetentionFactKeyStatementsPrepareLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for _, keys := range factkeys.Deletes() {
		for form, statement := range map[string]string{"generation": keys.Generation, "page": keys.Page} {
			prepared, err := database.PrepareContext(ctx, statement)
			if err != nil {
				t.Fatalf("prepare %s %s delete: %v", keys.Table, form, err)
			}
			_ = prepared.Close()
		}
	}
}

// TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive pins the plan
// of every #7420 key sidecar delete to its (scope_id, generation_id, fact_id)
// partition index with both leading columns bound, under no planner
// statistics, after ANALYZE, and under a forced generic plan, and bounds the
// executed buffers by the rows deleted. A planner flip to a sequential scan or
// to a fact_id-only primary key walk, whose cost grows with the table, fails.
// The seeded REDs prove the check sees both hazards: the rejected fact_id-only
// page form, and the page form with its partition index dropped.
func TestGenerationRetentionFactKeyPlansStayOnPartitionIndexLive(t *testing.T) {
	bridgeRetentionFamilyDSN(t)
	database, ctx := openGenerationRetentionMigratedSchema(t)
	for _, table := range []string{"fact_records", "relationship_reference_candidate_keys", "package_manifest_consumption_keys", "package_registry_identity_keys"} {
		if _, err := database.ExecContext(ctx, "ALTER TABLE "+table+" SET (autovacuum_enabled = false)"); err != nil {
			t.Fatalf("disable autovacuum on %s: %v", table, err)
		}
	}
	seedFactKeyPlanCorpus(t, ctx, database)
	const scope, generation = "plan-scope-3", "plan-scope-3-gen-2"
	page := factKeyPlanPageIDs(t, ctx, database, scope, generation)

	check := func(t *testing.T, condition string, generic bool) {
		t.Helper()
		for _, keys := range factkeys.Deletes() {
			for form, statement := range map[string]string{"generation": keys.Generation, "page": keys.Page} {
				args := []any{scope, generation}
				if form == "page" {
					args = append(args, page)
				}
				plan := explainFactKeyDelete(t, ctx, database, statement, args, generic)
				if violations := factKeyPlanViolations(t, plan, keys.Table); len(violations) > 0 {
					t.Errorf("%s %s %s delete leaves its partition index: %s\n%s", condition, keys.Table, form, strings.Join(violations, "; "), plan)
				}
			}
		}
	}
	t.Run("cold-custom", func(t *testing.T) { check(t, "cold custom", false) })
	t.Run("cold-generic", func(t *testing.T) { check(t, "cold generic", true) })
	for _, table := range []string{"fact_records", "relationship_reference_candidate_keys", "package_manifest_consumption_keys", "package_registry_identity_keys"} {
		if _, err := database.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
	t.Run("analyzed-custom", func(t *testing.T) { check(t, "analyzed custom", false) })
	t.Run("analyzed-generic", func(t *testing.T) { check(t, "analyzed generic", true) })
	t.Run("executed-buffers-track-the-generation", func(t *testing.T) {
		for _, keys := range factkeys.Deletes() {
			for form, statement := range map[string]string{"generation": keys.Generation, "page": keys.Page} {
				args := []any{scope, generation}
				if form == "page" {
					args = append(args, page)
				}
				rows, buffers := executeFactKeyDelete(t, ctx, database, statement, args)
				if rows == 0 || buffers > 4*rows+64 {
					t.Errorf("%s %s delete removed %d rows with %d buffers, want rows > 0 and buffers <= 4*rows+64", keys.Table, form, rows, buffers)
				}
				t.Logf("%s %s delete: %d rows, %d buffers", keys.Table, form, rows, buffers)
			}
		}
	})
	t.Run("seeded-red-fact-id-only-page-form", func(t *testing.T) {
		statement := "DELETE FROM package_registry_identity_keys WHERE fact_id = ANY($1::text[])"
		plan := explainFactKeyDelete(t, ctx, database, statement, []any{page}, false)
		if violations := factKeyPlanViolations(t, plan, "package_registry_identity_keys"); len(violations) == 0 {
			t.Fatalf("plan guard accepted the rejected fact_id-only page delete:\n%s", plan)
		}
	})
	t.Run("seeded-red-missing-partition-index", func(t *testing.T) {
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("open connection: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(ctx, "BEGIN; DROP INDEX package_manifest_consumption_keys_partition_idx"); err != nil {
			t.Fatalf("drop partition index: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
		var plan []byte
		if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+factkeys.Deletes()[1].Page, scope, generation, page).Scan(&plan); err != nil {
			t.Fatalf("explain without partition index: %v", err)
		}
		if violations := factKeyPlanViolations(t, plan, "package_manifest_consumption_keys"); len(violations) == 0 {
			t.Fatalf("plan guard accepted a page delete with no partition index:\n%s", plan)
		}
	})
}

func seedFactKeyPlanCorpus(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status)
SELECT 'plan-scope-' || s, 'repository', 'git', 'plan-' || s, 'git', 'plan-' || s, now(), now(), 'active'
FROM generate_series(1, $1::int) AS s`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
SELECT 'plan-scope-' || s || '-gen-' || g, 'plan-scope-' || s, 'snapshot', now(), now(), 'superseded', now()
FROM generate_series(1, $1::int) AS s CROSS JOIN generate_series(1, $2::int) AS g`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT md5(generation.generation_id || '-' || n), generation.scope_id, generation.generation_id, 'content_entity',
    'k-' || n, 'git', 'k-' || n, now(), now(), '{}'::jsonb
FROM scope_generations AS generation CROSS JOIN generate_series(1, $1::int) AS n
WHERE generation.scope_id LIKE 'plan-scope-%'`,
		`INSERT INTO relationship_reference_candidate_keys (fact_id, scope_id, generation_id, source_repo_id, reference_key)
SELECT fact_id, scope_id, generation_id, 'repo-plan', '|' || fact_id || '|' FROM fact_records WHERE scope_id LIKE 'plan-scope-%'`,
		`INSERT INTO package_manifest_consumption_keys (fact_id, scope_id, generation_id, repository_id, ecosystem, package_name)
SELECT fact_id, scope_id, generation_id, 'repo-plan', 'npm', 'pkg-' || (hashtext(fact_id) & 255)
FROM fact_records WHERE scope_id LIKE 'plan-scope-%' AND (hashtext(fact_id || 'm') & 2147483647) % 100 < 20`,
		`INSERT INTO package_registry_identity_keys (fact_id, scope_id, generation_id, ecosystem, package_name, package_id)
SELECT fact_id, scope_id, generation_id, 'npm', 'pkg-' || (hashtext(fact_id) & 255), 'pid-' || fact_id
FROM fact_records WHERE scope_id LIKE 'plan-scope-%' AND (hashtext(fact_id || 'p') & 2147483647) % 100 < 5`,
	}
	args := [][]any{{factKeyPlanScopes}, {factKeyPlanScopes, factKeyPlanGenerations}, {factKeyPlanFacts}, nil, nil, nil}
	for i, statement := range statements {
		if _, err := database.ExecContext(ctx, statement, args[i]...); err != nil {
			t.Fatalf("seed plan corpus step %d: %v", i, err)
		}
	}
}

// factKeyPlanPageIDs returns the first chunk-shaped page of the generation:
// fact ids ordered by (observed_at, fact_id), as the chunk page query pages.
func factKeyPlanPageIDs(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) []string {
	t.Helper()
	rows, err := database.QueryContext(ctx, generationRetentionChunkPageQuery, scopeID, generationID, factKeyPlanPage)
	if err != nil {
		t.Fatalf("page facts: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id, kind, repoID, path, entityID string
		var tombstone bool
		if err := rows.Scan(&id, &kind, &tombstone, &repoID, &path, &entityID); err != nil {
			t.Fatalf("scan page fact: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil || len(ids) != factKeyPlanPage {
		t.Fatalf("page facts = %d (%v), want %d", len(ids), err, factKeyPlanPage)
	}
	return ids
}

// explainFactKeyDelete plans statement with args bound (custom) or as a
// prepared statement under plan_cache_mode=force_generic_plan (generic).
func explainFactKeyDelete(t *testing.T, ctx context.Context, database *sql.DB, statement string, args []any, generic bool) []byte {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var plan []byte
	if !generic {
		if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+statement, args...).Scan(&plan); err != nil {
			t.Fatalf("explain custom plan: %v", err)
		}
		return plan
	}
	types := []string{"text", "text", "text[]"}[:len(args)]
	if _, err := conn.ExecContext(ctx, "BEGIN; SET LOCAL plan_cache_mode = force_generic_plan; PREPARE fact_key_probe("+strings.Join(types, ", ")+") AS "+statement); err != nil {
		t.Fatalf("prepare generic plan: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE fact_key_probe; ROLLBACK") }()
	// EXPLAIN EXECUTE takes no bind parameters, so the values go inline.
	literals := make([]string, len(args))
	for i, arg := range args {
		switch value := arg.(type) {
		case string:
			literals[i] = "'" + strings.ReplaceAll(value, "'", "''") + "'"
		case []string:
			literals[i] = "'{" + strings.Join(value, ",") + "}'::text[]"
		default:
			t.Fatalf("unsupported generic plan argument %T", arg)
		}
	}
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE fact_key_probe("+strings.Join(literals, ", ")+")").Scan(&plan); err != nil {
		t.Fatalf("explain generic plan: %v", err)
	}
	return plan
}

// executeFactKeyDelete runs statement under EXPLAIN (ANALYZE, BUFFERS) in a
// rolled-back transaction and returns the rows deleted and the delete node's
// shared buffers (hit plus read).
func executeFactKeyDelete(t *testing.T, ctx context.Context, database *sql.DB, statement string, args []any) (int64, int64) {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var plan []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, args...).Scan(&plan); err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	var root []struct {
		Plan struct {
			Plans []struct {
				ActualRows float64 `json:"Actual Rows"`
			} `json:"Plans"`
			SharedHit  int64 `json:"Shared Hit Blocks"`
			SharedRead int64 `json:"Shared Read Blocks"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 || len(root[0].Plan.Plans) != 1 {
		t.Fatalf("decode analyzed plan: %v\n%s", err, plan)
	}
	return int64(root[0].Plan.Plans[0].ActualRows), root[0].Plan.SharedHit + root[0].Plan.SharedRead
}

// factKeyPlanViolations reports each access to table that is not a scan of
// its partition index bound on both scope_id and generation_id.
func factKeyPlanViolations(t *testing.T, plan []byte, table string) []string {
	t.Helper()
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	var violations []string
	accesses := 0
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		condition, _ := node["Index Cond"].(string)
		switch {
		case relation == table && nodeType == "Seq Scan":
			violations = append(violations, "Seq Scan on "+table)
		case index != "" && strings.HasPrefix(index, table):
			accesses++
			if index != table+"_partition_idx" {
				violations = append(violations, fmt.Sprintf("%s on %s", nodeType, index))
			} else if !strings.Contains(condition, "scope_id =") || !strings.Contains(condition, "generation_id =") {
				violations = append(violations, fmt.Sprintf("%s on %s without scope and generation (%s)", nodeType, index, condition))
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root[0].Plan)
	if accesses == 0 && len(violations) == 0 {
		violations = append(violations, "never probes "+table+"_partition_idx")
	}
	return violations
}
