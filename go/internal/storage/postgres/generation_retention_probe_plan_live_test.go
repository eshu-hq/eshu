// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// retentionPlanAllowedFactIndexes are the only fact_records access paths the
// retention statements may use (#7279): the (scope_id, generation_id) prefix
// indexes that enumerate candidate facts, and the two key indexes that probe
// for retained holders. Anything else (a Seq Scan, or a skip scan over an
// unrelated index) is a pass over the table whose cost grows with it.
var retentionPlanAllowedFactIndexes = []string{
	"fact_records_scope_generation_idx",
	"fact_records_collector_status_active_idx",
	"fact_records_content_entity_key_idx",
	"fact_records_file_key_idx",
}

// retentionStatement is one retention statement under test, with the key
// indexes its plan must probe.
type retentionStatement struct {
	name     string
	sql      string
	required []string
}

// retentionPlanStatements are the statements the scope lock is held across,
// with the key index each must probe.
var retentionPlanStatements = []retentionStatement{
	{"row_counts", generationRetentionRowCountsQuery, []string{"fact_records_content_entity_key_idx", "fact_records_file_key_idx"}},
	{"prune_refs", pruneContentFileReferencesForGenerationsQuery, []string{"fact_records_file_key_idx"}},
	{"prune_entities", pruneContentEntitiesForGenerationsQuery, []string{"fact_records_content_entity_key_idx"}},
	{"prune_files", pruneContentFilesForGenerationsQuery, []string{"fact_records_file_key_idx"}},
}

// TestGenerationRetentionProbePlansStayOnKeyIndexesLive fails when a retention
// statement's plan reads fact_records by any path whose cost grows with the
// table, under the two conditions that broke earlier shapes: no planner
// statistics at all (a freshly bulk-loaded database) and a forced generic plan
// (what a prepared statement settles on). Then it drops a key index inside a
// rolled-back transaction and requires the same check to fail, so the guard is
// proven able to see a plan that lost its probe.
func TestGenerationRetentionProbePlansStayOnKeyIndexesLive(t *testing.T) {
	database, ctx := openGenerationRetentionMigratedSchema(t)
	disableRetentionProbeAutovacuum(t, ctx, database)
	corpus := retentionProbeCorpus{prefix: "p", scopes: 8, superseded: 5, entities: 400, files: 80, filler: 100, old: true}
	seedRetentionProbeCorpus(t, ctx, database, corpus)
	candidates := retentionProbeCandidates(corpus, 3)

	check := func(t *testing.T, condition string, explain func(string) []byte) {
		t.Helper()
		for _, statement := range retentionPlanStatements {
			plan := explain(statement.sql)
			if violations := retentionPlanViolations(t, plan, statement.required); len(violations) > 0 {
				t.Errorf("%s %s plan leaves the key probe: %s\n%s", condition, statement.name, strings.Join(violations, "; "), plan)
			}
		}
	}
	t.Run("cold-custom", func(t *testing.T) {
		check(t, "cold custom", func(statement string) []byte {
			return explainRetentionCustom(t, ctx, database, statement, candidates)
		})
	})
	t.Run("cold-generic", func(t *testing.T) {
		check(t, "cold generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	analyzeRetentionProbeTables(t, ctx, database)
	t.Run("analyzed-generic", func(t *testing.T) {
		check(t, "analyzed generic", func(statement string) []byte {
			return explainRetentionGeneric(t, ctx, database, statement, candidates)
		})
	})
	t.Run("seeded-red-missing-key-index", func(t *testing.T) {
		conn, err := database.Conn(ctx)
		if err != nil {
			t.Fatalf("open connection: %v", err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.ExecContext(ctx, "BEGIN; DROP INDEX fact_records_content_entity_key_idx"); err != nil {
			t.Fatalf("drop key index: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
		var plan []byte
		if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+pruneContentEntitiesForGenerationsQuery, candidates).Scan(&plan); err != nil {
			t.Fatalf("explain without key index: %v", err)
		}
		if violations := retentionPlanViolations(t, plan, []string{"fact_records_content_entity_key_idx"}); len(violations) == 0 {
			t.Fatalf("plan guard accepted an entity prune with no key index:\n%s", plan)
		}
	})
}

// explainRetentionCustom plans statement with the candidate ids bound, as the
// store's parameterized call does on its first executions.
func explainRetentionCustom(t *testing.T, ctx context.Context, database *sql.DB, statement string, candidates []string) []byte {
	t.Helper()
	var plan []byte
	if err := database.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+statement, candidates).Scan(&plan); err != nil {
		t.Fatalf("explain custom plan: %v", err)
	}
	return plan
}

// explainRetentionGeneric plans statement as a prepared statement under
// plan_cache_mode=force_generic_plan, the plan a reused prepared statement
// settles on, which cannot see the candidate ids.
func explainRetentionGeneric(t *testing.T, ctx context.Context, database *sql.DB, statement string, candidates []string) []byte {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN; SET LOCAL plan_cache_mode = force_generic_plan; PREPARE retention_probe(text[]) AS "+statement); err != nil {
		t.Fatalf("prepare generic plan: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE retention_probe; ROLLBACK") }()
	literal := "'{" + strings.Join(candidates, ",") + "}'::text[]"
	var plan []byte
	if err := conn.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE retention_probe("+literal+")").Scan(&plan); err != nil {
		t.Fatalf("explain generic plan: %v", err)
	}
	return plan
}

// retentionPlanViolations walks an EXPLAIN (FORMAT JSON) plan and reports each
// fact_records access whose cost grows with the table: a Seq Scan, an index
// outside retentionPlanAllowedFactIndexes, a key index read without the key, or
// a (scope_id, generation_id) index read without both columns. It also reports
// each required key index the plan never uses.
func retentionPlanViolations(t *testing.T, plan []byte, required []string) []string {
	t.Helper()
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	var violations []string
	used := map[string]bool{}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		if index != "" {
			used[index] = true
		}
		condition, _ := node["Index Cond"].(string)
		factIndex := index != "" && (relation == "fact_records" || strings.HasPrefix(index, "fact_records"))
		switch {
		case relation == "fact_records" && nodeType == "Seq Scan":
			violations = append(violations, "Seq Scan on fact_records")
		case factIndex && !slices.Contains(retentionPlanAllowedFactIndexes, index):
			violations = append(violations, fmt.Sprintf("%s on %s", nodeType, index))
		case factIndex && strings.HasSuffix(index, "_key_idx") && !strings.Contains(condition, "'repo_id'"):
			// A key index read without the key is a full index pass.
			violations = append(violations, fmt.Sprintf("%s on %s without a key condition", nodeType, index))
		case factIndex && !strings.HasSuffix(index, "_key_idx") &&
			(!strings.Contains(condition, "scope_id =") || !strings.Contains(condition, "generation_id =")):
			// A (scope_id, generation_id) index read without both columns is a
			// skip scan over every scope: its cost grows with the table.
			violations = append(violations, fmt.Sprintf("%s on %s without scope and generation (%s)", nodeType, index, condition))
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root[0].Plan)
	for _, index := range required {
		if !used[index] {
			violations = append(violations, "never probes "+index)
		}
	}
	return violations
}
