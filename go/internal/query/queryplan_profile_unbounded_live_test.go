// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// TestQueryplanProfileFlagsUnboundedVarLength is the seeded proof for #7335:
// on the pinned queryplan image, PROFILE of an unbounded CALLS traversal must
// be flagged by assertNoUnboundedVarLength's detector, and the same traversal
// with an upper bound must not. It runs against the same isolated database as
// the production PROFILE gate and writes only its own two Function nodes.
func TestQueryplanProfileFlagsUnboundedVarLength(t *testing.T) {
	if os.Getenv(queryplanProfileLiveEnv) != "1" {
		t.Skipf("set %s=1 to run live PROFILE assertions", queryplanProfileLiveEnv)
	}
	if os.Getenv(queryplanProfileIsolatedEnv) != "1" {
		t.Fatal("ESHU_QUERYPLAN_PROFILE_ISOLATED=1 is required because this test writes nodes")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	auth := neo4jdriver.NoAuth()
	if username := strings.TrimSpace(os.Getenv("ESHU_NEO4J_USERNAME")); username != "" {
		auth = neo4jdriver.BasicAuth(username, os.Getenv("ESHU_NEO4J_PASSWORD"), "")
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, auth)
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()
	ctx, cancel := context.WithTimeout(t.Context(), queryplanProfileQueryBudget)
	defer cancel()
	config := neo4jdriver.ExecuteQueryWithDatabase(strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE")))
	seed := "MERGE (a:Function {uid: 'qp-7335-a'}) MERGE (b:Function {uid: 'qp-7335-b'}) MERGE (a)-[:CALLS]->(b)"
	if _, err := neo4jdriver.ExecuteQuery(ctx, driver, seed, nil, neo4jdriver.EagerResultTransformer, config); err != nil {
		t.Fatalf("seed CALLS pair: %v", err)
	}

	tests := []struct {
		name      string
		cypher    string
		unbounded bool
	}{
		{"unbounded star", "MATCH (a:Function {uid: $uid})-[:CALLS*]->(b) RETURN b.uid", true},
		{"lower bound only", "MATCH (a:Function {uid: $uid})-[:CALLS*1..]->(b) RETURN b.uid", true},
		{"upper bound", "MATCH (a:Function {uid: $uid})-[:CALLS*1..4]->(b) RETURN b.uid", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := neo4jdriver.ExecuteQuery(ctx, driver, "PROFILE "+tt.cypher,
				map[string]any{"uid": "qp-7335-a"}, neo4jdriver.EagerResultTransformer, config)
			if err != nil {
				t.Fatalf("PROFILE %s: %v", tt.name, err)
			}
			found := queryplanUnboundedVarLengthOperators(result.Summary.Profile())
			if got := len(found) > 0; got != tt.unbounded {
				t.Fatalf("unbounded detection = %v (%v), want %v", got, found, tt.unbounded)
			}
		})
	}
}

// queryplanUnboundedVarLengthOperators walks a profiled plan and returns one
// "operator: details" entry for every unbounded variable-length expansion.
func queryplanUnboundedVarLengthOperators(plan neo4jdriver.ProfiledPlan) []string {
	if plan == nil {
		return nil
	}
	var found []string
	details := fmt.Sprint(plan.Arguments()["Details"])
	if queryplanUnboundedVarLength(plan.Operator(), details) {
		found = append(found, plan.Operator()+": "+details)
	}
	for _, child := range plan.Children() {
		found = append(found, queryplanUnboundedVarLengthOperators(child)...)
	}
	return found
}

// assertNoUnboundedVarLength fails the test when the profiled plan expands a
// variable-length path with no upper bound (#7335).
func assertNoUnboundedVarLength(t *testing.T, name string, plan neo4jdriver.ProfiledPlan) {
	t.Helper()
	if found := queryplanUnboundedVarLengthOperators(plan); len(found) > 0 {
		t.Fatalf("%s expands a variable-length path with no upper bound: %v", name, found)
	}
}
