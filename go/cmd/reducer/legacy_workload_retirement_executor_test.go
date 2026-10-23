// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// countingRunner reports a node-delete counter the way neo4jSessionRunner does
// after a committed statement, and can fail first to exercise the retry seam.
type countingRunner struct {
	errs    []error
	deleted int64
	calls   int
}

func (r *countingRunner) RunCypher(ctx context.Context, cypher string, params map[string]any) error {
	i := r.calls
	r.calls++
	if i < len(r.errs) && r.errs[i] != nil {
		return r.errs[i]
	}
	sourcecypher.ReportWriteCounts(ctx, cypher, params, sourcecypher.WriteCounters{NodesDeleted: r.deleted, RelationshipsDeleted: 9})
	return nil
}

func (r *countingRunner) RunCypherGroup(context.Context, []sourcecypher.Statement) error {
	return errors.New("not used")
}

func TestReducerCypherExecutorCountsNodeDeletesAndForwardsToAnOuterCollector(t *testing.T) {
	t.Parallel()
	runner := &countingRunner{deleted: 3}
	executor := newReducerCypherExecutor(runner, nil, nil)

	outer := sourcecypher.NewWriteCountsCollector()
	ctx := sourcecypher.WithWriteCountsCollector(context.Background(), outer)
	got, err := executor.ExecuteCypherCountingNodeDeletes(ctx, "MATCH (w:Workload) WHERE w.id IN $ids DETACH DELETE w", map[string]any{"ids": []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Fatalf("deleted = %d, want the node counter (3), not the relationship counter", got)
	}
	if len(outer.Entries()) != 1 {
		t.Fatalf("outer collector entries = %d, want 1 forwarded entry", len(outer.Entries()))
	}
}

func TestReducerCypherExecutorCountingNodeDeletesRetriesATransientErrorOnce(t *testing.T) {
	t.Parallel()
	transient := &neo4jdriver.Neo4jError{Code: "Neo.TransientError.Transaction.DeadlockDetected", Msg: "deadlock"}
	runner := &countingRunner{errs: []error{transient}, deleted: 1}
	executor := newReducerCypherExecutor(runner, nil, nil)

	got, err := executor.ExecuteCypherCountingNodeDeletes(context.Background(), "MATCH (w:Workload) WHERE w.id IN $ids DETACH DELETE w", map[string]any{"ids": []string{"a"}})
	if err != nil || got != 1 || runner.calls != 2 {
		t.Fatalf("deleted=%d err=%v calls=%d, want 1 delete after one retried deadlock", got, err, runner.calls)
	}
}

type stubRetirementExecutor struct{ deleted int64 }

func (stubRetirementExecutor) ExecuteCypher(context.Context, string, map[string]any) error {
	return nil
}

func (e stubRetirementExecutor) ExecuteCypherCountingNodeDeletes(context.Context, string, map[string]any) (int64, error) {
	return e.deleted, nil
}

// emptyGraph reads as a clean graph: no legacy candidates.
type emptyGraph struct{}

func (emptyGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func postLegacyRetirement(t *testing.T, backend runtimecfg.GraphBackend, body string) *httptest.ResponseRecorder {
	t.Helper()
	handler, err := legacyWorkloadRetirementHandler(stubRetirementExecutor{}, nil, emptyQuery{}, backend, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/retire-legacy-workloads", strings.NewReader(body)))
	return rec
}

// emptyQuery satisfies query.GraphQuery for the decorated-reader fallback.
type emptyQuery struct{ emptyGraph }

func (emptyQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestLegacyWorkloadRetirementExecuteIsNeo4jOnlyAndDryRunWorksOnBoth(t *testing.T) {
	t.Parallel()
	for _, backend := range []runtimecfg.GraphBackend{runtimecfg.GraphBackendNeo4j, runtimecfg.GraphBackendNornicDB} {
		if rec := postLegacyRetirement(t, backend, `{"dry_run": true}`); rec.Code != http.StatusOK {
			t.Errorf("%s dry run status = %d body = %s", backend, rec.Code, rec.Body)
		}
	}
	if rec := postLegacyRetirement(t, runtimecfg.GraphBackendNeo4j, `{"dry_run": false, "reason": "r"}`); rec.Code != http.StatusOK {
		t.Errorf("neo4j execute status = %d body = %s", rec.Code, rec.Body)
	}
	if rec := postLegacyRetirement(t, runtimecfg.GraphBackendNornicDB, `{"dry_run": false, "reason": "r"}`); rec.Code != http.StatusNotImplemented {
		t.Errorf("nornicdb execute status = %d, want 501", rec.Code)
	}
}
