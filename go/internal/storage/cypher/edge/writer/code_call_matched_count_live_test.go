// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// liveDriver opens the graph backend named by the standard local-testing
// variables and skips when ESHU_NEO4J_URI is unset, so graph-free
// environments stay green; a set but unreachable backend fails.
func liveDriver(tb testing.TB) (neo4jdriver.DriverWithContext, string) {
	tb.Helper()

	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		tb.Skip("ESHU_NEO4J_URI not set; skipping code-call matched-count live test")
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
	return driver, database
}

// liveCountedExecutor is a driver-backed counted-write executor for the live
// test: it runs each statement, collects the MatchedCountColumn RETURN row,
// and consumes the summary — the same collect-then-consume order as the
// production reducer adapter.
type liveCountedExecutor struct {
	driver   neo4jdriver.DriverWithContext
	database string
}

func (e *liveCountedExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return err
	}
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	return nil
}

func (e *liveCountedExecutor) ExecuteCountedWrite(ctx context.Context, stmt sourcecypher.Statement) (int64, error) {
	session := e.driver.NewSession(ctx, neo4jdriver.SessionConfig{
		AccessMode:   neo4jdriver.AccessModeWrite,
		DatabaseName: e.database,
	})
	defer func() { _ = session.Close(ctx) }()

	result, err := session.Run(ctx, stmt.Cypher, stmt.Parameters)
	if err != nil {
		return 0, err
	}
	var count int64
	rows := 0
	for result.Next(ctx) {
		record := result.Record()
		if record == nil {
			continue
		}
		value, found := record.Get(sourcecypher.MatchedCountColumn)
		if !found {
			return 0, fmt.Errorf("statement did not return %q", sourcecypher.MatchedCountColumn)
		}
		n, ok := sourcecypher.CountProbeIntValue(value)
		if !ok {
			return 0, fmt.Errorf("statement returned a non-integer %q", sourcecypher.MatchedCountColumn)
		}
		count = n
		rows++
	}
	if err := result.Err(); err != nil {
		return 0, err
	}
	if _, err := result.Consume(ctx); err != nil {
		return 0, err
	}
	if rows != 1 {
		return 0, fmt.Errorf("statement returned %d rows, want exactly 1", rows)
	}
	return count, nil
}

func (e *liveCountedExecutor) ExecuteCountedWriteGroup(ctx context.Context, stmts []sourcecypher.Statement) ([]int64, error) {
	counts := make([]int64, 0, len(stmts))
	for _, stmt := range stmts {
		count, err := e.ExecuteCountedWrite(ctx, stmt)
		if err != nil {
			return nil, err
		}
		counts = append(counts, count)
	}
	return counts, nil
}

func liveCodeCallRow(intentID, tag string, payload map[string]any) sharedintent.Row {
	return sharedintent.Row{
		IntentID:     intentID,
		RepositoryID: "repo-app",
		PartitionKey: "p0",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Payload:      payload,
	}
}

// TestCodeCallMatchedCountLive is the #10938 live proof on Neo4j: every base
// code-call template reports matched==submitted in steady state with resolved
// endpoints (the D6 census for CALLS, REFERENCES, USES_METACLASS, and
// INSTANTIATES on this backend), and a CALLS batch with an absent callee
// parks instead of completing with zero edges.
func TestCodeCallMatchedCountLive(t *testing.T) {
	driver, database := liveDriver(t)
	ctx := context.Background()
	tag := strings.ReplaceAll(t.Name(), "/", "_") + ":10938"

	executor := &liveCountedExecutor{driver: driver, database: database}
	// Exercise the production retry forwarding around the driver seam.
	retrying := &sourcecypher.RetryingExecutor{Inner: executor}
	writer := NewEdgeWriter(retrying, 0)

	seed := `MERGE (:Function {uid: $caller}) MERGE (:Class {uid: $callee}) MERGE (:Function {uid: $meta}) MERGE (:Class {uid: $metatarget})`
	seedStmt := sourcecypher.Statement{Cypher: seed, Parameters: map[string]any{
		"caller": tag + ":caller", "callee": tag + ":callee",
		"meta": tag + ":meta", "metatarget": tag + ":metatarget",
	}}
	if err := executor.Execute(ctx, seedStmt); err != nil {
		t.Fatalf("seed nodes: %v", err)
	}
	t.Cleanup(func() {
		session := driver.NewSession(context.Background(), neo4jdriver.SessionConfig{
			AccessMode:   neo4jdriver.AccessModeWrite,
			DatabaseName: database,
		})
		defer func() { _ = session.Close(context.Background()) }()
		_, _ = session.Run(context.Background(),
			`MATCH (n) WHERE n.uid STARTS WITH $prefix DETACH DELETE n`,
			map[string]any{"prefix": tag + ":"})
	})

	payloads := map[string]map[string]any{
		"CALLS": {
			"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":callee",
		},
		"REFERENCES": {
			"relationship_type": "REFERENCES",
			"caller_entity_id":  tag + ":caller", "callee_entity_id": tag + ":callee",
		},
		"USES_METACLASS": {
			"relationship_type": "USES_METACLASS",
			"source_entity_id":  tag + ":meta", "target_entity_id": tag + ":metatarget",
		},
		"INSTANTIATES": {
			"relationship_type": "INSTANTIATES",
			"caller_entity_id":  tag + ":caller", "callee_entity_id": tag + ":callee",
		},
	}
	sources := map[string]string{
		"CALLS": "parser/code-calls", "REFERENCES": "parser/code-calls",
		"USES_METACLASS": "parser/python-metaclass", "INSTANTIATES": "parser/code-calls",
	}
	for _, relationship := range []string{"CALLS", "REFERENCES", "USES_METACLASS", "INSTANTIATES"} {
		report, err := writer.WriteEdges(ctx, reducercontract.DomainCodeCalls,
			[]sharedintent.Row{liveCodeCallRow("live-"+relationship, tag, payloads[relationship])},
			sources[relationship])
		if err != nil {
			t.Fatalf("WriteEdges(%s) steady state = %v, want full match", relationship, err)
		}
		if report.SubmittedRows != 1 || report.MatchedRows != 1 {
			t.Fatalf("WriteEdges(%s) report = %+v, want SubmittedRows=1 MatchedRows=1 (D6 census)", relationship, report)
		}
	}

	// The issue's race signature: probe clear, callee absent at write.
	absent := map[string]any{
		"caller_entity_id": tag + ":caller", "callee_entity_id": tag + ":no-such-callee",
	}
	report, err := writer.WriteEdges(ctx, reducercontract.DomainCodeCalls,
		[]sharedintent.Row{liveCodeCallRow("live-absent", tag, absent)},
		"parser/code-calls")
	if err == nil {
		t.Fatal("WriteEdges() with an absent callee succeeded, want the park error so the intent stays open")
	}
	if report.SubmittedRows != 1 || report.MatchedRows != 0 {
		t.Fatalf("report = %+v, want SubmittedRows=1 MatchedRows=0", report)
	}

	// The parked edge appears after the callee phase publishes, with no
	// re-ingest: seed the callee, rewrite, and read the edge back.
	if err := executor.Execute(ctx, sourcecypher.Statement{
		Cypher:     `MERGE (:Function {uid: $callee})`,
		Parameters: map[string]any{"callee": tag + ":no-such-callee"},
	}); err != nil {
		t.Fatalf("seed late callee: %v", err)
	}
	report, err = writer.WriteEdges(ctx, reducercontract.DomainCodeCalls,
		[]sharedintent.Row{liveCodeCallRow("live-absent", tag, absent)},
		"parser/code-calls")
	if err != nil {
		t.Fatalf("WriteEdges() after the callee published = %v, want nil (retry converges)", err)
	}
	if report.SubmittedRows != 1 || report.MatchedRows != 1 {
		t.Fatalf("report = %+v, want SubmittedRows=1 MatchedRows=1 after the callee published", report)
	}
}
