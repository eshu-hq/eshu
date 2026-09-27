// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestBuildReducerServiceWiresRepositoryEdgeReader pins the production
// composition root for the #7285 stale-edge guard. The workload materialization
// handler keeps a nil RepositoryEdgeReader working (it falls back to the
// unguarded keep-list DELETE, which is correct but pays NornicDB#296 on every
// run), so no handler-level test notices buildReducerService dropping the
// RepositoryEdgeReader line. This drives a full-generation
// workload_materialization intent through the service buildReducerService
// returns and requires the graph reader it was given to serve the guard's
// DEFINES and EXPOSES_ENDPOINT reads for the retracting repository.
func TestBuildReducerServiceWiresRepositoryEdgeReader(t *testing.T) {
	t.Parallel()

	database := &repositoryEdgeReaderWiringDB{}
	graph := &recordingEdgeGraphReader{}
	service, err := buildReducerService(
		context.Background(), database, stubGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(database), stubCypherReader{}, graph,
		func(string) string { return "" }, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	now := time.Now().UTC()
	_, execErr := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:        "repository-edge-reader-wiring",
		Domain:          reducer.DomainWorkloadMaterialization,
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Cause:           "facts projected",
		EntityKeys:      []string{"repo-edge-api"},
		RelatedScopeIDs: []string{"repository:test-scope"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          reducer.IntentStatusPending,
	})
	if execErr != nil {
		t.Fatalf("Executor.Execute() error = %v, want nil", execErr)
	}

	for _, shape := range []string{":DEFINES]", ":EXPOSES_ENDPOINT]"} {
		if !graph.readForRepository(shape, "repo-edge-api") {
			t.Fatalf("no %s guard read for repo-edge-api reached the production graph reader: "+
				"buildReducerService dropped RepositoryEdgeReader, so #7285 retracts run the unguarded DELETE\n%s",
				shape, graph.describe())
		}
	}
}

// recordingEdgeGraphReader is a graph reader that records every read so the
// wiring test can tell which handler used it.
type recordingEdgeGraphReader struct {
	stubCypherReader
	mu    sync.Mutex
	calls []recordedEdgeRead
}

type recordedEdgeRead struct {
	cypher string
	params map[string]any
}

func (r *recordingEdgeGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedEdgeRead{cypher: cypher, params: params})
	return nil, nil
}

// readForRepository reports whether a recorded read matches shape and named
// repoID in its repo_ids parameter.
func (r *recordingEdgeGraphReader) readForRepository(shape, repoID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		ids, _ := call.params["repo_ids"].([]string)
		if !strings.Contains(call.cypher, shape) {
			continue
		}
		for _, id := range ids {
			if id == repoID {
				return true
			}
		}
	}
	return false
}

func (r *recordingEdgeGraphReader) describe() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for _, call := range r.calls {
		b.WriteString("read: " + strings.Join(strings.Fields(call.cypher), " ") + "\n")
	}
	return b.String()
}

// repositoryEdgeReaderWiringDB serves one full-generation repository fact and
// nothing else, so workload materialization takes its zero-candidate path and
// retracts every stale repository edge for that repository.
type repositoryEdgeReaderWiringDB struct {
	fakeReducerDB
}

func (f *repositoryEdgeReaderWiringDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	if strings.Contains(query, "FROM fact_records\n") {
		payload, err := json.Marshal(map[string]any{"graph_id": "repo-edge-api", "name": "edge-api"})
		if err != nil {
			panic(err)
		}
		return &crossScopeReadinessRows{rows: [][]any{
			deployableUnitQuiescenceFactRow("fact-repository", "repository", "repo-edge-api", payload, time.Now().UTC()),
		}}, nil
	}
	if strings.Contains(query, "FROM resolved_relationships") {
		return &crossScopeReadinessRows{}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}
