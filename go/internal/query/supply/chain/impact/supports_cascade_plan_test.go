// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// supportsGenerationIndex is the index the generation foreign key's cascade
// delete must use. Migration 336 creates it for that one statement.
const supportsGenerationIndex = "supply_chain_impact_finding_supports_generation_idx"

// supportsCascadeStatement is the statement the foreign key's ON DELETE
// CASCADE trigger runs for each deleted generation (PostgreSQL's ri_triggers
// plan it as DELETE FROM ONLY <table> WHERE <fk column> = $1).
const supportsCascadeStatement = `DELETE FROM ONLY supply_chain_impact_finding_supports WHERE generation_id = $1`

// cascadePlan is one EXPLAIN (FORMAT JSON) tree of the cascade statement.
type cascadePlan struct {
	root planNode
}

// scans labels every access to the supports table, ModifyTable excluded.
func (p cascadePlan) scans() []string {
	var out []string
	for _, access := range supportsTableAccesses(p.root) {
		out = append(out, strings.TrimSpace(access.NodeType+" "+access.IndexName+" ["+access.IndexCond+"]"))
	}
	return out
}

// supportsTableAccesses returns every node that reads the supports table: scan
// nodes on the relation, and the Bitmap Index Scan nodes beneath a Bitmap Heap
// Scan on it (the Index Cond lives on those). ModifyTable names the relation
// it deletes from but reads nothing, so it is skipped.
func supportsTableAccesses(node planNode) []planNode {
	var out []planNode
	if node.RelationName == supportsPlanTable && node.NodeType != "ModifyTable" {
		if node.NodeType == "Bitmap Heap Scan" {
			out = append(out, bitmapIndexNodes(node)...)
		} else {
			out = append(out, node)
		}
	}
	for _, child := range node.Plans {
		out = append(out, supportsTableAccesses(child)...)
	}
	return out
}

// bitmapIndexNodes lists the Bitmap Index Scan nodes under node.
func bitmapIndexNodes(node planNode) []planNode {
	var out []planNode
	for _, child := range node.Plans {
		if child.NodeType == "Bitmap Index Scan" {
			out = append(out, child)
		}
		out = append(out, bitmapIndexNodes(child)...)
	}
	return out
}

// supportsCascadeUsesGenerationIndex reports whether every read of the supports
// table in the cascade plan is an index scan on supportsGenerationIndex with
// generation_id in its Index Cond; it returns the reason when not. A plan that
// reads the table through the (scope_id, generation_id) index (a full pass per
// deleted generation) or by a sequential scan fails.
func supportsCascadeUsesGenerationIndex(plan cascadePlan) (bool, string) {
	accesses := supportsTableAccesses(plan.root)
	if len(accesses) == 0 {
		return false, "the plan never scans the supports table"
	}
	for _, access := range accesses {
		switch access.NodeType {
		case "Index Scan", "Index Only Scan", "Bitmap Index Scan":
		default:
			return false, fmt.Sprintf("%s reads the supports table", access.NodeType)
		}
		if access.IndexName != supportsGenerationIndex {
			return false, fmt.Sprintf("%s uses index %q", access.NodeType, access.IndexName)
		}
		if !strings.Contains(access.IndexCond, "generation_id") {
			return false, fmt.Sprintf("index condition %q does not name generation_id", access.IndexCond)
		}
	}
	return true, ""
}

// parseCascadePlan decodes EXPLAIN (FORMAT JSON) output.
func parseCascadePlan(raw []byte) (cascadePlan, error) {
	var plan []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return cascadePlan{}, err
	}
	if len(plan) != 1 {
		return cascadePlan{}, fmt.Errorf("explain returned %d plans", len(plan))
	}
	return cascadePlan{root: plan[0].Plan}, nil
}

// explainSupportsCascadePlan explains the cascade statement the way the
// foreign key trigger runs it: prepared, under a generic plan (the trigger
// reuses one plan across generations). With dropIndex it drops the generation
// index first, in a transaction that always rolls back, so the caller can prove
// the checker rejects the plan an unindexed table gets.
func explainSupportsCascadePlan(ctx context.Context, database *sql.DB, dropIndex bool) (cascadePlan, error) {
	conn, err := database.Conn(ctx)
	if err != nil {
		return cascadePlan{}, err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		return cascadePlan{}, err
	}
	defer func() { _, _ = conn.ExecContext(ctx, "RESET plan_cache_mode") }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return cascadePlan{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if dropIndex {
		if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS "+supportsGenerationIndex); err != nil {
			return cascadePlan{}, err
		}
	}
	const name = "supports_cascade_probe"
	if _, err := tx.ExecContext(ctx, "PREPARE "+name+"(text) AS "+supportsCascadeStatement); err != nil {
		return cascadePlan{}, err
	}
	defer func() { _, _ = conn.ExecContext(ctx, "DEALLOCATE ALL") }()
	var raw []byte
	if err := tx.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) EXECUTE "+name+"('gen:plan:7')").Scan(&raw); err != nil {
		return cascadePlan{}, err
	}
	return parseCascadePlan(raw)
}

// TestSupportsCascadeChecker proves the checker on canned plans: it accepts a
// generation_id index probe and rejects every shape that costs a table-sized
// read per deleted generation.
func TestSupportsCascadeChecker(t *testing.T) {
	const modify = `"Node Type":"ModifyTable","Relation Name":"supply_chain_impact_finding_supports"`
	cases := []struct {
		name string
		plan string
		want bool
	}{
		{"generation index probe", `[{"Plan":{` + modify + `,"Plans":[{"Node Type":"Index Scan","Relation Name":"supply_chain_impact_finding_supports","Index Name":"` + supportsGenerationIndex + `","Index Cond":"(generation_id = $1)"}]}}]`, true},
		{"bitmap on the generation index", `[{"Plan":{` + modify + `,"Plans":[{"Node Type":"Bitmap Heap Scan","Relation Name":"supply_chain_impact_finding_supports","Plans":[{"Node Type":"Bitmap Index Scan","Index Name":"` + supportsGenerationIndex + `","Index Cond":"(generation_id = $1)"}]}]}}]`, true},
		{"full pass of the scope index", `[{"Plan":{` + modify + `,"Plans":[{"Node Type":"Index Scan","Relation Name":"supply_chain_impact_finding_supports","Index Name":"supply_chain_impact_finding_supports_scope_generation_idx","Index Cond":"(generation_id = $1)"}]}}]`, false},
		{"sequential scan", `[{"Plan":{` + modify + `,"Plans":[{"Node Type":"Seq Scan","Relation Name":"supply_chain_impact_finding_supports"}]}}]`, false},
		{"generation index without the key", `[{"Plan":{` + modify + `,"Plans":[{"Node Type":"Index Scan","Relation Name":"supply_chain_impact_finding_supports","Index Name":"` + supportsGenerationIndex + `","Index Cond":"(scope_id = $1)"}]}}]`, false},
		{"one good scan and one bad", `[{"Plan":{"Node Type":"Append","Plans":[{"Node Type":"Index Scan","Relation Name":"supply_chain_impact_finding_supports","Index Name":"` + supportsGenerationIndex + `","Index Cond":"(generation_id = $1)"},{"Node Type":"Seq Scan","Relation Name":"supply_chain_impact_finding_supports"}]}}]`, false},
		{"no scan of the table", `[{"Plan":{` + modify + `}}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := parseCascadePlan([]byte(tc.plan))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got, reason := supportsCascadeUsesGenerationIndex(plan); got != tc.want {
				t.Fatalf("checker = %v (%s), want %v", got, reason, tc.want)
			}
		})
	}
}
