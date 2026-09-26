// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestBuildReducerServiceWiresDeployableUnitBoundExpiryLogger guards the
// production composition root for the #7268 operator signal: when a shared-edge
// target stays absent past the wait bound, the deployable-unit handler's WARN
// must reach the reducer's configured logger (the structured telemetry handler
// main wires), not slog.Default(), which nothing in production configures. The
// handler keeps a nil Logger working for focused tests, so only this test can
// detect the wiring being dropped.
func TestBuildReducerServiceWiresDeployableUnitBoundExpiryLogger(t *testing.T) {
	t.Parallel()

	var buf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	database := &deployableUnitTargetBoundDB{}
	service, err := buildReducerService(
		context.Background(), database, &absentTargetProbeExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(database), stubCypherReader{}, stubCypherReader{},
		func(string) string { return "" }, nil, nil, logger, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	pastBound := time.Now().UTC().Add(-crossscope.ProducerReadinessMaxWait - time.Minute)
	_, execErr := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:        "deployable-unit-target-bound-wiring",
		Domain:          reducer.DomainDeployableUnitCorrelation,
		ScopeID:         "repository:test-scope",
		GenerationID:    "generation-456",
		SourceSystem:    "git",
		EntityKeys:      []string{"repo-edge-api"},
		RelatedScopeIDs: []string{"repository:deploy-repo"},
		CycleStartedAt:  pastBound,
		EnqueuedAt:      pastBound,
	})
	if execErr == nil {
		t.Fatal("execution error = nil, want the bounded, counting target-absent failure")
	}
	if !database.readDeployableUnitInputs {
		t.Fatalf("handler never read its resolved relationships (execErr=%v): the fixture did not reach the edge write", execErr)
	}
	if !strings.Contains(execErr.Error(), "target still absent") {
		t.Fatalf("execution error = %v, want the bound-expiry error", execErr)
	}

	out := buf.String()
	for _, want := range []string{
		"level=WARN",
		"shared edge target absent past the wait bound",
		"domain=deployable_unit_correlation",
		"scope_id=repository:test-scope",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("reducer logger missing %q: the bound-expiry WARN did not reach the configured logger:\n%s", want, out)
		}
	}
}

// lockedBuffer is a goroutine-safe log sink: buildReducerService wires the same
// logger into background runners.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// absentTargetProbeExecutor is a graph executor whose target-presence probe
// always reports the deployment Repository absent, the state the shared-edge
// writer turns into its non-counting target-not-ready deferral.
type absentTargetProbeExecutor struct{}

func (*absentTargetProbeExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (*absentTargetProbeExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

func (*absentTargetProbeExecutor) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	return false, nil
}

// deployableUnitTargetBoundDB answers every Postgres read the deployable-unit
// handler makes before its edge write: an active own relationship generation, a
// quiescent canonical graph, one candidate-bearing fact set, and a complete
// corpus fence carrying one deploys-from relationship.
type deployableUnitTargetBoundDB struct {
	fakeReducerDB
	readDeployableUnitInputs bool
}

func (f *deployableUnitTargetBoundDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	switch {
	// The fused fence-and-read statement embeds the generation-status
	// predicate, so it must match before the own-generation readiness probe.
	case strings.Contains(query, "WITH fence AS MATERIALIZED"):
		f.readDeployableUnitInputs = true
		return &resolvedRelationshipRows{rows: [][]any{deployableUnitFencedResolvedRow()}}, nil
	case strings.Contains(query, "graph_projection_phase_state AS phase"):
		return &fakeExistsRows{value: false}, nil
	case strings.Contains(query, "FROM relationship_generations") && strings.Contains(query, "status = 'active'"):
		return &fakeExistsRows{value: true}, nil
	case strings.Contains(query, "FROM fact_records\n"):
		return &crossScopeReadinessRows{rows: deployableUnitQuiescenceFactRows()}, nil
	case strings.Contains(query, "FROM resolved_relationships"):
		return &resolvedRelationshipRows{}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

// deployableUnitFencedResolvedRow is the corpus-fenced by-repos row shape:
// complete verdict, source/target repo and entity keys, relationship type,
// confidence, evidence count, rationale, resolution source, details.
func deployableUnitFencedResolvedRow() []any {
	details, err := json.Marshal(map[string]any{
		"evidence_kinds": []string{"ARGOCD_APPLICATION_SOURCE"},
	})
	if err != nil {
		panic(err)
	}
	return []any{
		true,
		sql.NullString{String: "repo-deployments", Valid: true},
		sql.NullString{String: "repo-edge-api", Valid: true},
		sql.NullString{String: "repo-deployments", Valid: true},
		sql.NullString{String: "repo-edge-api", Valid: true},
		sql.NullString{String: "DEPLOYS_FROM", Valid: true},
		0.94,
		int64(1),
		"argocd application source",
		"inferred",
		details,
	}
}

// resolvedRelationshipRows is a db.Rows over resolved_relationships fixture
// rows, supporting the nullable and float scan targets the store uses.
type resolvedRelationshipRows struct {
	rows  [][]any
	index int
}

func (r *resolvedRelationshipRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *resolvedRelationshipRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan destination count %d does not match fixture width %d", len(dest), len(row))
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *bool:
			*target = row[i].(bool)
		case *sql.NullString:
			*target = row[i].(sql.NullString)
		case *string:
			*target = row[i].(string)
		case *float64:
			*target = row[i].(float64)
		case *int:
			*target = int(row[i].(int64))
		case *int64:
			*target = row[i].(int64)
		case *[]byte:
			*target = row[i].([]byte)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func (r *resolvedRelationshipRows) Err() error   { return nil }
func (r *resolvedRelationshipRows) Close() error { return nil }
