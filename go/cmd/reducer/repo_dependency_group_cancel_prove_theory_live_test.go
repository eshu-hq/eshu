// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestLiveRepoDependencyGroupedCancellationRollsBackProveTheory(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_REPO_GROUP_CANCEL_PROVE_LIVE")) == "" {
		t.Skip("set ESHU_REPO_GROUP_CANCEL_PROVE_LIVE=1 to run the grouped cancellation shim")
	}
	uri := strings.TrimSpace(os.Getenv("ESHU_NEO4J_URI"))
	if uri == "" {
		t.Fatal("ESHU_NEO4J_URI is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := neo4jdriver.NewDriverWithContext(uri, neo4jdriver.NoAuth())
	if err != nil {
		t.Fatalf("open graph driver: %v", err)
	}
	defer func() { _ = driver.Close(context.Background()) }()

	probePrefix := fmt.Sprintf("repo-group-cancel-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = runRepoDependencyGroupProbe(
			cleanupCtx,
			neo4jSessionRunner{Driver: driver},
			`MATCH (n:RepoDependencyGroupCancelProbe) WHERE n.probe STARTS WITH $probe DETACH DELETE n`,
			map[string]any{"probe": probePrefix},
		)
	})

	controlProbe := probePrefix + "-control"
	control := neo4jSessionRunner{Driver: driver, TxTimeout: 5 * time.Second}
	if err := control.RunCypherGroup(ctx, []sourcecypher.Statement{{
		Cypher:     `UNWIND range(1, 3) AS i CREATE (:RepoDependencyGroupCancelProbe {probe: $probe, ordinal: i})`,
		Parameters: map[string]any{"probe": controlProbe},
	}}); err != nil {
		t.Fatalf("uncanceled grouped negative control: %v", err)
	}
	if count := countRepoDependencyGroupProbe(t, ctx, driver, controlProbe); count != 3 {
		t.Fatalf("uncanceled grouped negative control wrote %d nodes, want 3", count)
	}

	for _, tc := range []struct {
		name        string
		txTimeout   time.Duration
		callTimeout time.Duration
	}{
		{name: "transaction_timeout_metadata", txTimeout: time.Millisecond, callTimeout: 10 * time.Second},
		{name: "caller_context_cancel", txTimeout: 10 * time.Second, callTimeout: time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := probePrefix + "-" + tc.name
			callCtx, callCancel := context.WithTimeout(ctx, tc.callTimeout)
			err := (neo4jSessionRunner{Driver: driver, TxTimeout: tc.txTimeout}).RunCypherGroup(
				callCtx,
				[]sourcecypher.Statement{
					{
						Cypher:     `UNWIND range(1, 50000) AS i CREATE (:RepoDependencyGroupCancelProbe {probe: $probe, ordinal: i})`,
						Parameters: map[string]any{"probe": probe},
					},
					{
						Cypher:     `MATCH (n:RepoDependencyGroupCancelProbe {probe: $probe}) SET n.group_finished = true`,
						Parameters: map[string]any{"probe": probe},
					},
				},
			)
			callCancel()
			if err == nil {
				t.Fatal("slow grouped write unexpectedly completed inside its timeout")
			}

			time.Sleep(500 * time.Millisecond)
			if count := countRepoDependencyGroupProbe(t, ctx, driver, probe); count != 0 {
				t.Fatalf("canceled grouped write committed %d nodes after timeout", count)
			}
		})
	}
}

func runRepoDependencyGroupProbe(
	ctx context.Context,
	runner neo4jSessionRunner,
	query string,
	params map[string]any,
) error {
	return runner.RunCypher(ctx, query, params)
}

func countRepoDependencyGroupProbe(
	t *testing.T,
	ctx context.Context,
	driver neo4jdriver.DriverWithContext,
	probe string,
) int64 {
	t.Helper()
	session := driver.NewSession(ctx, neo4jdriver.SessionConfig{AccessMode: neo4jdriver.AccessModeRead})
	defer func() { _ = session.Close(context.Background()) }()
	result, err := session.Run(ctx,
		`MATCH (n:RepoDependencyGroupCancelProbe {probe: $probe}) RETURN count(n) AS count`,
		map[string]any{"probe": probe})
	if err != nil {
		t.Fatalf("read grouped cancel probe: %v", err)
	}
	record, err := result.Single(ctx)
	if err != nil {
		t.Fatalf("read grouped cancel probe row: %v", err)
	}
	value, _ := record.Get("count")
	count, _ := value.(int64)
	return count
}
