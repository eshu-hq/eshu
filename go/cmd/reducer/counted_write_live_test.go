// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// openCountedLiveRunner connects to the graph backend named by the standard
// local-testing variables (docs/public/reference/local-testing.md: Common
// Compose Environment) and returns a neo4jSessionRunner against it. It skips
// when ESHU_NEO4J_URI is unset, so graph-free environments stay green; a set
// but unreachable backend fails, so a misconfigured live lane cannot pass by
// skipping.
func openCountedLiveRunner(tb testing.TB) neo4jSessionRunner {
	tb.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		tb.Skip("ESHU_NEO4J_URI not set; skipping counted-write live test")
	}
	username := strings.TrimSpace(os.Getenv("NEO4J_USERNAME"))
	if username == "" {
		username = "neo4j"
	}
	password := os.Getenv("NEO4J_PASSWORD")
	database := strings.TrimSpace(os.Getenv("ESHU_NEO4J_DATABASE"))
	if database == "" {
		database = "neo4j"
	}
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.BasicAuth(username, password, ""))
	if err != nil {
		tb.Fatalf("open neo4j driver %q: %v", uri, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		tb.Fatalf("verify neo4j connectivity %q: %v", uri, err)
	}
	tb.Cleanup(func() { _ = driver.Close(context.Background()) })
	return neo4jSessionRunner{Driver: driver, DatabaseName: database}
}

// TestNeo4jSessionRunnerCountedWriteLive is the #10938 live proof (3) on
// Neo4j: a `RETURN count(...)` tail on a mutating UNWIND+MATCH+MERGE+SET
// statement reports exactly the rows whose MATCH bound, atomically with the
// write — one row back with the count, never zero rows, and the count
// survives Consume.
func TestNeo4jSessionRunnerCountedWriteLive(t *testing.T) {
	runner := openCountedLiveRunner(t)
	ctx := context.Background()
	tag := strings.ReplaceAll(t.Name(), "/", "_")

	seed := `MERGE (a:Function {uid: $caller}) MERGE (b:Function {uid: $callee})`
	if err := runner.RunCypher(ctx, seed, map[string]any{
		"caller": tag + ":caller",
		"callee": tag + ":callee",
	}); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	t.Cleanup(func() {
		_ = runner.RunCypher(context.Background(),
			`MATCH (n:Function) WHERE n.uid STARTS WITH $prefix DETACH DELETE n`,
			map[string]any{"prefix": tag + ":"})
	})

	present := `UNWIND $rows AS row
MATCH (source:Function {uid: row.caller_entity_id})
MATCH (target:Function {uid: row.callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = row.confidence
RETURN count(rel) AS matched`
	count, err := runner.RunCypherCountedWrite(ctx, present, map[string]any{
		"rows": []map[string]any{
			{"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":callee", "confidence": 0.9},
			{"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":callee", "confidence": 0.9},
		},
	})
	if err != nil {
		t.Fatalf("RunCypherCountedWrite() with both endpoints present = %v, want count 2", err)
	}
	if count != 2 {
		t.Fatalf("matched = %d, want 2 (both rows bound)", count)
	}

	absent := `UNWIND $rows AS row
MATCH (source:Function {uid: row.caller_entity_id})
MATCH (target:Function {uid: row.callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = row.confidence
RETURN count(rel) AS matched`
	count, err = runner.RunCypherCountedWrite(ctx, absent, map[string]any{
		"rows": []map[string]any{
			{"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":no-such-callee", "confidence": 0.9},
		},
	})
	if err != nil {
		t.Fatalf("RunCypherCountedWrite() with an absent callee = %v, want count 0 (write succeeds, binds nothing)", err)
	}
	if count != 0 {
		t.Fatalf("matched = %d, want 0 (callee absent: the #10938 race signature)", count)
	}

	grouped, err := runner.RunCypherCountedWriteGroup(ctx, []sourcecypher.Statement{
		{Cypher: present, Parameters: map[string]any{
			"rows": []map[string]any{
				{"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":callee", "confidence": 0.9},
			},
		}},
		{Cypher: absent, Parameters: map[string]any{
			"rows": []map[string]any{
				{"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":no-such-callee", "confidence": 0.9},
			},
		}},
	})
	if err != nil {
		t.Fatalf("RunCypherCountedWriteGroup() = %v", err)
	}
	if len(grouped) != 2 || grouped[0] != 1 || grouped[1] != 0 {
		t.Fatalf("grouped counts = %v, want [1 0] aligned with stmts", grouped)
	}
}
