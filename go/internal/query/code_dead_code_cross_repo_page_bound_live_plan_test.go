// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The EXPLAIN plumbing TestCrossRepoDeadCodeConsumerEvidencePageBoundLive's
// guards share: running the shipped page statement under EXPLAIN (ANALYZE,
// FORMAT JSON) in either plan mode, reading the plan cache's choice after a run
// of executions, and walking the plan tree. JSON rather than text because the
// guards need each node's parent and whether it sits in a SubPlan, which the
// text form only carries as indentation.

// crossRepoDeadCodeConsumerPagePlanNode is one node of an EXPLAIN (ANALYZE,
// FORMAT JSON) plan, narrowed to the fields the guards read. Actual Rows and
// Rows Removed by Filter are per-loop averages, so the work a node did is
// (ActualRows + RowsRemovedByFilter) * ActualLoops.
type crossRepoDeadCodeConsumerPagePlanNode struct {
	NodeType            string                                  `json:"Node Type"`
	ParentRelationship  string                                  `json:"Parent Relationship"`
	RelationName        string                                  `json:"Relation Name"`
	IndexName           string                                  `json:"Index Name"`
	PresortedKey        []string                                `json:"Presorted Key"`
	ActualRows          float64                                 `json:"Actual Rows"`
	ActualLoops         float64                                 `json:"Actual Loops"`
	RowsRemovedByFilter float64                                 `json:"Rows Removed by Filter"`
	Plans               []crossRepoDeadCodeConsumerPagePlanNode `json:"Plans"`
}

// entries is the index or heap entries the node walked across every loop: the
// rows it passed up plus the rows its filter discarded.
func (n crossRepoDeadCodeConsumerPagePlanNode) entries() float64 {
	return (n.ActualRows + n.RowsRemovedByFilter) * n.ActualLoops
}

// crossRepoDeadCodeConsumerPagePlanVisit calls visit for every node with the
// chain of its ancestors, root first.
func crossRepoDeadCodeConsumerPagePlanVisit(
	node crossRepoDeadCodeConsumerPagePlanNode,
	ancestors []crossRepoDeadCodeConsumerPagePlanNode,
	visit func(node crossRepoDeadCodeConsumerPagePlanNode, ancestors []crossRepoDeadCodeConsumerPagePlanNode),
) {
	visit(node, ancestors)
	chain := append(append([]crossRepoDeadCodeConsumerPagePlanNode{}, ancestors...), node)
	for _, child := range node.Plans {
		crossRepoDeadCodeConsumerPagePlanVisit(child, chain, visit)
	}
}

// crossRepoDeadCodeConsumerPagePlan runs the shipped page statement under
// EXPLAIN (ANALYZE, FORMAT JSON) in the given plan mode and returns the plan's
// root node together with the raw JSON, which failure messages print.
func crossRepoDeadCodeConsumerPagePlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode crossRepoDeadCodeProbePlanMode,
	page []string,
	grant []string,
) (crossRepoDeadCodeConsumerPagePlanNode, string) {
	t.Helper()

	const prefix = "EXPLAIN (ANALYZE, FORMAT JSON) "
	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery("repo-producer", page, grant)
	statement := prefix + query
	if mode.generic {
		statement = crossRepoDeadCodeConsumerPageGenericStatement(ctx, t, db, prefix, query, args)
		args = nil
	}
	plan, raw := crossRepoDeadCodeConsumerPageExplain(ctx, t, db, statement, args)
	// A generic plan leaves the producer repository a parameter marker where a
	// custom plan inlines it, so a refactor that stopped forcing the mode
	// cannot leave two subtests asking the planner the same question twice.
	if mode.generic && !strings.Contains(raw, "repository_id <> $1") {
		t.Fatalf("plan was not built generically -- the producer repository is not a parameter in it:\n%s", raw)
	}
	if !mode.generic && !strings.Contains(raw, "repository_id <> 'repo-producer'::text") {
		t.Fatalf("plan was not built with the values in hand:\n%s", raw)
	}
	return plan, raw
}

// crossRepoDeadCodeConsumerPageExplain runs one EXPLAIN (FORMAT JSON) statement
// and decodes its plan.
func crossRepoDeadCodeConsumerPageExplain(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	statement string,
	args []any,
) (crossRepoDeadCodeConsumerPagePlanNode, string) {
	t.Helper()

	var raw string
	if err := db.QueryRowContext(ctx, statement, args...).Scan(&raw); err != nil {
		t.Fatalf("explain the page read: %v", err)
	}
	var decoded []struct {
		Plan crossRepoDeadCodeConsumerPagePlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("decode the page read's plan (err %v, %d plans):\n%s", err, len(decoded), raw)
	}
	return decoded[0].Plan, raw
}

// crossRepoDeadCodeConsumerPagePrepare prepares the page statement on the pinned
// connection and returns its name plus the EXECUTE argument list. The parameter
// types are read off the bound arguments rather than written out, so the same
// helper prepares whatever argument shape the shipped builder binds: a string is
// text, an encoded array is text[].
func crossRepoDeadCodeConsumerPagePrepare(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	query string,
	args []any,
) (string, string) {
	t.Helper()

	name := fmt.Sprintf("cross_repo_dead_code_page_%d", time.Now().UnixNano())
	types := make([]string, 0, len(args))
	values := make([]string, 0, len(args))
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			types = append(types, "text")
			values = append(values, crossRepoDeadCodeProbeQuoteLiteral(value))
		case driver.Valuer:
			encoded, err := value.Value()
			text, ok := encoded.(string)
			if err != nil || !ok {
				t.Fatalf("encode page argument %#v: %v", arg, err)
			}
			types = append(types, "text[]")
			values = append(values, crossRepoDeadCodeProbeQuoteLiteral(text))
		default:
			t.Fatalf("page argument %#v has a type this helper cannot prepare", arg)
		}
	}
	if _, err := db.ExecContext(ctx, "PREPARE "+name+"("+strings.Join(types, ", ")+") AS "+query); err != nil {
		t.Fatalf("prepare the page read: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "DEALLOCATE "+name); err != nil {
			t.Errorf("deallocate the page read: %v", err)
		}
	})
	return name, strings.Join(values, ", ")
}

// crossRepoDeadCodeConsumerPageGenericStatement prepares the page statement and
// forces a plan built without its values, returning an EXPLAIN of an EXECUTE
// with nothing left to bind.
func crossRepoDeadCodeConsumerPageGenericStatement(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	prefix string,
	query string,
	args []any,
) string {
	t.Helper()

	if _, err := db.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force a generic plan: %v", err)
	}
	// Registered before the PREPARE, and separately from it: the pool is pinned
	// to one connection, so a failed PREPARE must still leave plan_cache_mode
	// reset for every statement after it.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "RESET plan_cache_mode"); err != nil {
			t.Errorf("reset plan_cache_mode: %v", err)
		}
	})
	name, values := crossRepoDeadCodeConsumerPagePrepare(ctx, t, db, query, args)
	return prefix + "EXECUTE " + name + "(" + values + ")"
}

// crossRepoDeadCodeConsumerPageCachedPlan is what Postgres's plan cache serves
// this statement after a run of executions under the default plan_cache_mode.
//
// pgx prepares these reads server-side, so after five executions the cache may
// switch to a generic plan: the plan the reader really runs is whichever one
// the cache settled on, not the one a single EXPLAIN gets. The statement is
// prepared, executed twelve times with the page's values, the cache's
// generic/custom counts are read, and the thirteenth execution is EXPLAINed.
func crossRepoDeadCodeConsumerPageCachedPlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	page []string,
	grant []string,
) (crossRepoDeadCodeConsumerPagePlanNode, string, int, int) {
	t.Helper()

	query, args := buildCrossRepoDeadCodeConsumerEvidenceQuery("repo-producer", page, grant)
	name, values := crossRepoDeadCodeConsumerPagePrepare(ctx, t, db, query, args)
	const executions = 12
	for i := 0; i < executions; i++ {
		rows, err := db.QueryContext(ctx, "EXECUTE "+name+"("+values+")")
		if err != nil {
			t.Fatalf("execute the prepared page read (%d): %v", i+1, err)
		}
		returned := 0
		for rows.Next() {
			returned++
		}
		if err := rows.Err(); err != nil || returned == 0 {
			t.Fatalf("drain the prepared page read (%d): %v", i+1, err)
		}
		_ = rows.Close()
	}
	var generic, custom int
	if err := db.QueryRowContext(ctx,
		"SELECT generic_plans, custom_plans FROM pg_prepared_statements WHERE name = $1", name,
	).Scan(&generic, &custom); err != nil {
		t.Fatalf("read the plan cache's counts: %v", err)
	}
	if generic+custom != executions {
		t.Fatalf("plan cache counted %d generic + %d custom plans, want %d executions in all", generic, custom, executions)
	}
	plan, raw := crossRepoDeadCodeConsumerPageExplain(ctx, t, db,
		"EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE "+name+"("+values+")", nil)
	return plan, raw, generic, custom
}
