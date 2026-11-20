// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// TestBuildReducerServiceKeepsScopeWorkloadsForForeignKeyedIntent pins the
// production composition root for the #7285 scope-wide keep-list (arbiter
// option B). A workload_materialization intent whose entity keys name a
// repository outside its scope (a repo_dependency or deployment_mapping
// replay) writes nothing for the scope's repository, but it still retracts
// for every full-generation repository in the scope. Its keep-list must be
// the scope generation's admitted workloads, so the production service must
// read the guard and issue no DELETE for the current DEFINES edge. A
// composition root that wires an input loader without scope truth skips the
// retract instead, which this test also catches: no guard read happens.
func TestBuildReducerServiceKeepsScopeWorkloadsForForeignKeyedIntent(t *testing.T) {
	t.Parallel()

	workloadID := workloadid.NewWorkloadID("repo-edge-api", "edge-api").String()
	graph := &scopeKeepEdgeGraphReader{definesTarget: workloadID}
	executor := &scopeKeepRecordingExecutor{}
	service, err := buildReducerService(
		context.Background(), &scopeKeepWiringDB{}, stubGraphExecutor{}, executor,
		postgres.NewSharedIntentStore(&scopeKeepWiringDB{}), stubCypherReader{}, graph,
		obligationConsumerDisabledGetenv(func(string) string { return "" }), nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	now := time.Now().UTC()
	if _, err := service.Executor.Execute(context.Background(), reducercontract.Intent{
		IntentID:        "scope-keep-wiring",
		Domain:          reducer.DomainWorkloadMaterialization,
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		Cause:           "repo dependency replay",
		EntityKeys:      []string{"repo:repo-somewhere-else"},
		RelatedScopeIDs: []string{"repository:test-scope"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          reducer.IntentStatusPending,
	}); err != nil {
		t.Fatalf("Executor.Execute() error = %v, want nil", err)
	}

	if !graph.readDefines() {
		t.Fatal("no DEFINES guard read reached the production graph reader: the production input " +
			"loader supplied no scope truth, so the #7285 retract was skipped")
	}
	if deletes := executor.definesDeletes(); len(deletes) > 0 {
		t.Fatalf("foreign-keyed intent deleted the scope's current DEFINES edge to %s: %d DELETE statement(s)\n%s",
			workloadID, len(deletes), strings.Join(deletes, "\n"))
	}
}

// scopeKeepEdgeGraphReader answers the retract guard's DEFINES read with one
// current edge from repo-edge-api to definesTarget.
type scopeKeepEdgeGraphReader struct {
	stubCypherReader
	definesTarget string
	mu            sync.Mutex
	defines       bool
}

func (r *scopeKeepEdgeGraphReader) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	if !strings.Contains(cypher, ":DEFINES]") {
		return nil, nil
	}
	r.mu.Lock()
	r.defines = true
	r.mu.Unlock()
	return []map[string]any{{"repo_id": "repo-edge-api", "target_id": r.definesTarget}}, nil
}

func (r *scopeKeepEdgeGraphReader) readDefines() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.defines
}

// scopeKeepRecordingExecutor records every DEFINES delete the workload
// materializer sends.
type scopeKeepRecordingExecutor struct {
	mu      sync.Mutex
	deletes []string
}

func (e *scopeKeepRecordingExecutor) ExecuteCypher(_ context.Context, cypher string, _ map[string]any) error {
	if strings.Contains(cypher, ":DEFINES]") && strings.Contains(cypher, "DELETE rel") {
		e.mu.Lock()
		e.deletes = append(e.deletes, strings.Join(strings.Fields(cypher), " "))
		e.mu.Unlock()
	}
	return nil
}

func (e *scopeKeepRecordingExecutor) ExecuteCypherGroup(ctx context.Context, statements []reducer.CypherGroupStatement) error {
	for _, statement := range statements {
		if err := e.ExecuteCypher(ctx, statement.Cypher, statement.Parameters); err != nil {
			return err
		}
	}
	return nil
}

func (e *scopeKeepRecordingExecutor) definesDeletes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.deletes...)
}

// scopeKeepWiringDB serves one full-generation repository with an admitted
// workload candidate: an ArgoCD Application declaring a Deployment.
type scopeKeepWiringDB struct {
	fakeReducerDB
}

func (f *scopeKeepWiringDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if strings.Contains(query, "FROM fact_records\n") {
		now := time.Now().UTC()
		return &crossScopeReadinessRows{rows: [][]any{
			deployableUnitQuiescenceFactRow("fact-repository", "repository", "repo-edge-api",
				scopeKeepJSON(map[string]any{"graph_id": "repo-edge-api", "name": "edge-api"}), now),
			deployableUnitQuiescenceFactRow("fact-application", "file", "repo-edge-api/deploy/application.yaml",
				scopeKeepJSON(map[string]any{
					"repo_id": "repo-edge-api", "relative_path": "deploy/application.yaml",
					"artifact_type": "argocd", "language": "yaml",
					"parsed_file_data": map[string]any{"k8s_resources": []any{
						map[string]any{"kind": "Deployment", "namespace": "prod"},
					}},
				}), now),
		}}, nil
	}
	// The restricted fused read (#10877) must match before the generic
	// generation-status branch: its blocking CTE also queries
	// relationship_generations with an active-status filter.
	if strings.Contains(query, "blocking AS MATERIALIZED") {
		return &scopeKeepFenceRows{}, nil
	}
	if strings.Contains(query, "WITH fence AS MATERIALIZED") {
		return &scopeKeepFenceRows{}, nil
	}
	if strings.Contains(query, "FROM relationship_generations") && strings.Contains(query, "status = 'active'") {
		return &fakeExistsRows{value: true}, nil
	}
	if strings.Contains(query, "FROM resolved_relationships") {
		return &crossScopeReadinessRows{}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

// scopeKeepFenceRows is the fused corpus-fence read for a complete corpus
// with no resolved relationship: one LEFT JOIN filler row, complete = true.
// On the restricted read (#10877) the verdict is followed by the
// verdict-time blocking set and, on the fingerprinted statement, the input
// fingerprint; the fixture answers with no blocking scopes and an empty
// fingerprint.
type scopeKeepFenceRows struct{ read bool }

func (r *scopeKeepFenceRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *scopeKeepFenceRows) Scan(dest ...any) error {
	// The fixture serves exactly the three fused shapes the store scans:
	// the global read (verdict + 10 resolved columns) and the restricted
	// read without (verdict + blocking + 10) and with (verdict + blocking
	// + fingerprint + 10) the fingerprint. Any other width is a shape
	// drift the fixture must surface, not absorb.
	switch len(dest) {
	case 11, 12, 13:
	default:
		return fmt.Errorf("scan: destination count %d, want a fused fence width (11, 12, or 13)", len(dest))
	}
	complete, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("scan: fence verdict dest %T, want *bool", dest[0])
	}
	*complete = true
	// The filler row carries no resolved relationship, so every column
	// dest takes its zero value; the store discards the row on the NULL
	// relationship_type.
	for _, d := range dest[1:] {
		switch target := d.(type) {
		case *array.StringArray:
			*target = array.StringArray{}
		case *string:
			*target = ""
		case *sql.NullString:
			*target = sql.NullString{}
		case *float64:
			*target = 0
		case *int:
			*target = 0
		case *[]byte:
			*target = nil
		default:
			return fmt.Errorf("scan: fence column dest %T is not a resolved column the fixture serves", d)
		}
	}
	return nil
}

func (r *scopeKeepFenceRows) Err() error   { return nil }
func (r *scopeKeepFenceRows) Close() error { return nil }

func scopeKeepJSON(value map[string]any) []byte {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return payload
}
