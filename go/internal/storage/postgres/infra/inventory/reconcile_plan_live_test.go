// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// TestReconcileRepositoriesLivePageStopsAtBudget proves a walk page costs
// O(budget), not O(repositories after the cursor): PostgreSQL evaluates a
// recursive CTE only as far as its consumer fetches, and each side's inner
// LIMIT stops the fetch. It seeds many more repositories than the budget and
// asserts from EXPLAIN ANALYZE that each Recursive Union produced at most
// budget+1 rows (the +1 is the seed row fetched before the limit is reached).
func TestReconcileRepositoriesLivePageStopsAtBudget(t *testing.T) {
	sqlDB, ctx := liveDB(t)
	const (
		repos  = 400
		budget = 10
	)
	// Derive every repository so both the content side and the table side
	// hold rows past the budget; a proof with an empty table side could not
	// catch the table side losing its inner LIMIT.
	prefix := uniqueRepo(t)
	database := postgres.SQLDB{DB: sqlDB}
	for i := range repos {
		seedDerivedRepo(t, ctx, database, fmt.Sprintf("%s-%04d", prefix, i),
			contentRow{id: "e", path: "main.tf", entityType: "TerraformResource", name: "r"})
	}

	var raw []byte
	if err := sqlDB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+inventory.ReconcileRepositoriesSQL,
		prefix, budget).Scan(&raw); err != nil {
		t.Fatalf("explain walk page: %v", err)
	}
	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan: %v (%d plans)", err, len(plans))
	}
	var unions []float64
	plans[0].Plan.walk(func(node planNode) {
		if node.NodeType == "Recursive Union" {
			unions = append(unions, node.ActualRows)
		}
	})
	if len(unions) != 2 {
		t.Fatalf("plan has %d Recursive Union nodes, want 2 (content and table sides)", len(unions))
	}
	for _, rows := range unions {
		if rows > budget+1 {
			t.Fatalf("a Recursive Union produced %.0f rows for budget %d over %d repositories; the page is not bounded by the budget",
				rows, budget, repos)
		}
	}
}

// planNode is the subset of a JSON EXPLAIN node the plan proofs read.
type planNode struct {
	NodeType   string     `json:"Node Type"`
	ActualRows float64    `json:"Actual Rows"`
	Plans      []planNode `json:"Plans"`
}

func (n planNode) walk(visit func(planNode)) {
	visit(n)
	for _, child := range n.Plans {
		child.walk(visit)
	}
}
