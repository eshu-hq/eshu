// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestLiveNeo4jIngesterCanonicalWriteTimeoutRequeues proves on a real Neo4j
// server that a timed-out ingester canonical write takes the same queue
// outcome as on NornicDB. The executor is the production
// canonicalExecutorForGraphBackend chain for Neo4j with
// ESHU_CANONICAL_WRITE_TIMEOUT=2s. A holder transaction keeps the write lock
// on the probe node, so the grouped canonical write blocks until the client
// deadline fires. The result must be a retryable graph_write_timeout, not a
// terminal error, and the abandoned server transaction must not commit once
// the holder releases the lock.
//
// Run with a disposable Neo4j (auth disabled):
//
//	ESHU_NEO4J_WRITE_TIMEOUT_LIVE=1 ESHU_NEO4J_URI=bolt://127.0.0.1:<port> \
//	  go test ./cmd/ingester -run TestLiveNeo4jIngesterCanonicalWriteTimeoutRequeues -count=1 -v
func TestLiveNeo4jIngesterCanonicalWriteTimeoutRequeues(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_NEO4J_WRITE_TIMEOUT_LIVE")) == "" {
		t.Skip("set ESHU_NEO4J_WRITE_TIMEOUT_LIVE=1 to run the Neo4j write-timeout proof")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	const configured = 2 * time.Second
	getenv := func(key string) string {
		if key == canonicalWriteTimeoutEnv {
			return configured.String()
		}
		return ""
	}
	writeTimeout := canonicalTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv)
	raw := ingesterNeo4jExecutor{Driver: driver, TxTimeout: writeTimeout}

	probe := fmt.Sprintf("neo4j-ingester-write-timeout-%d", time.Now().UnixNano())
	params := map[string]any{"probe": probe}
	if err := raw.Execute(ctx, sourcecypher.Statement{
		Cypher: `CREATE (:Neo4jWriteTimeoutProbe {probe: $probe, value: 'seed'})`, Parameters: params,
	}); err != nil {
		t.Fatalf("seed probe: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = raw.Execute(cleanupCtx, sourcecypher.Statement{
			Cypher: `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) DETACH DELETE n`, Parameters: params,
		})
	})

	holderSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeWrite})
	defer func() { _ = holderSession.Close(context.Background()) }()
	holder, err := holderSession.BeginTransaction(ctx)
	if err != nil {
		t.Fatalf("begin holder transaction: %v", err)
	}
	if _, err := holder.Run(ctx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'holder'`, params); err != nil {
		t.Fatalf("holder takes write lock: %v", err)
	}

	executor := canonicalExecutorForGraphBackend(
		raw,
		runtimecfg.GraphBackendNeo4j,
		writeTimeout,
		false,
		defaultNornicDBPhaseGroupStatements,
		defaultNornicDBFilePhaseStatements,
		defaultNornicDBStructuralEdgePhaseStatements,
		defaultNornicDBEntityPhaseStatements,
		nil,
		0,
		defaultNornicDBCanonicalRetractBatchSize,
		nil,
		nil,
		nil,
	)
	group, ok := executor.(sourcecypher.GroupExecutor)
	if !ok {
		t.Fatalf("executor %T does not expose grouped writes", executor)
	}
	start := time.Now()
	writeErr := group.ExecuteGroup(ctx, []sourcecypher.Statement{{
		Operation:  sourcecypher.OperationCanonicalUpsert,
		Cypher:     `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) SET n.value = 'contender'`,
		Parameters: params,
	}})
	elapsed := time.Since(start)
	t.Logf("canonical write returned after %s: %v", elapsed, writeErr)

	var timeoutErr sourcecypher.GraphWriteTimeoutError
	if !errors.As(writeErr, &timeoutErr) || !timeoutErr.Retryable() ||
		timeoutErr.FailureClass() != sourcecypher.GraphWriteTimeoutFailureClass {
		t.Fatalf("canonical write error = %v (%T), want retryable %s", writeErr, writeErr, sourcecypher.GraphWriteTimeoutFailureClass)
	}
	if elapsed < configured || elapsed > configured+10*time.Second {
		t.Fatalf("canonical write returned after %s, want about %s", elapsed, configured)
	}

	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
	// Give an abandoned server transaction time to take the released lock and
	// write, past both the client deadline and the server's own timeout.
	time.Sleep(2 * configured)
	readSession := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
	defer func() { _ = readSession.Close(context.Background()) }()
	result, err := readSession.Run(ctx, `MATCH (n:Neo4jWriteTimeoutProbe {probe: $probe}) RETURN n.value AS value`, params)
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read probe record: %v", err)
	}
	if value, _ := record.Get("value"); value != "holder" {
		t.Fatalf("probe value = %v, want holder: the timed-out write must not commit late", value)
	}
}
