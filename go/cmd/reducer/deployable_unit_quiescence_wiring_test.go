// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresDeployableUnitCanonicalQuiescenceFailClosed
// guards the production composition root for the #6184 deployable-unit
// readiness floor. The handler deliberately keeps this gate open when the seam
// is nil, so its focused tests cannot detect removal of this wiring.
func TestBuildReducerServiceWiresDeployableUnitCanonicalQuiescenceFailClosed(t *testing.T) {
	t.Parallel()

	quiescenceErr := errors.New("canonical quiescence unavailable")
	db := &deployableUnitQuiescenceWiringDB{quiescenceErr: quiescenceErr}
	service, err := buildReducerService(
		context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{},
		func(string) string { return "" }, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	_, execErr := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:        "deployable-unit-quiescence-wiring",
		Domain:          reducer.DomainDeployableUnitCorrelation,
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		EntityKeys:      []string{"repo-edge-api"},
		RelatedScopeIDs: []string{"repository:deploy-repo"},
	})
	if execErr == nil {
		t.Fatal("execution error = nil, want fail-closed canonical quiescence error")
	}

	if !errors.Is(execErr, quiescenceErr) {
		t.Fatalf("execution error = %v, want wrapped quiescence error", execErr)
	}
	if !strings.Contains(execErr.Error(), "check canonical repository quiescence") {
		t.Fatalf("execution error = %v, want canonical quiescence context", execErr)
	}
	if !db.probedCanonicalQuiescence {
		t.Fatal("canonical repository quiescence probe never ran: the production gate is not wired")
	}
	if db.resolvedRelationshipReads != 0 {
		t.Fatalf("resolved relationship reads = %d, want 0 after fail-closed quiescence error", db.resolvedRelationshipReads)
	}
}

// deployableUnitQuiescenceWiringDB provides one candidate-bearing generation,
// an active relationship generation, and a non-quiescent canonical graph.
type deployableUnitQuiescenceWiringDB struct {
	fakeReducerDB
	probedCanonicalQuiescence bool
	quiescenceErr             error
	resolvedRelationshipReads int
}

func (f *deployableUnitQuiescenceWiringDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	if strings.Contains(query, "graph_projection_phase_state AS phase") {
		f.probedCanonicalQuiescence = true
		return nil, f.quiescenceErr
	}
	if strings.Contains(query, "FROM relationship_generations") && strings.Contains(query, "status = 'active'") {
		return &fakeExistsRows{value: true}, nil
	}
	if strings.Contains(query, "FROM fact_records\n") {
		return &crossScopeReadinessRows{rows: deployableUnitQuiescenceFactRows()}, nil
	}
	if strings.Contains(query, "FROM resolved_relationships") {
		f.resolvedRelationshipReads++
		return &crossScopeReadinessRows{}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

func deployableUnitQuiescenceFactRows() [][]any {
	repositoryPayload, err := json.Marshal(map[string]any{
		"graph_id": "repo-edge-api",
		"name":     "edge-api",
	})
	if err != nil {
		panic(err)
	}
	filePayload, err := json.Marshal(map[string]any{
		"repo_id":       "repo-edge-api",
		"language":      "dockerfile",
		"relative_path": "Dockerfile",
		"parsed_file_data": map[string]any{
			"dockerfile_stages": []any{map[string]any{"name": "runtime"}},
		},
	})
	if err != nil {
		panic(err)
	}

	now := time.Now().UTC()
	return [][]any{
		deployableUnitQuiescenceFactRow("fact-repository", "repository", "repo-edge-api", repositoryPayload, now),
		deployableUnitQuiescenceFactRow("fact-dockerfile", "file", "repo-edge-api/Dockerfile", filePayload, now),
	}
}

func deployableUnitQuiescenceFactRow(
	factID string,
	factKind string,
	stableKey string,
	payload []byte,
	observedAt time.Time,
) []any {
	return []any{
		factID,
		"repository:test-scope",
		"generation-456",
		factKind,
		stableKey,
		"1.0.0",
		"git",
		int64(1),
		"reported",
		"git",
		stableKey,
		"",
		"",
		observedAt,
		false,
		payload,
	}
}
