// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresCrossScopeProducerReadiness guards the two lines
// in buildReducerService that hand the #5709 readiness floor its store and its
// logger.
//
// Nothing else notices if they go. The handler treats a nil readiness seam as
// "no floor" on purpose, so a deployment that has not adopted it keeps working
// rather than stranding every consumer -- which means deleting the wiring makes
// the floor inert in production while every unit test stays green. The floor's
// own tests run against a hand-built handler, not against this literal.
//
// So this drives the real buildReducerService, executes a
// ci_cd_run_correlation intent, and asserts the deferral actually comes out.
// The fake database answers the producer-quiescence probe with a registered
// oci_registry scope that has NOT finished publishing, which is the exact
// condition the floor exists to catch.
func TestBuildReducerServiceWiresCrossScopeProducerReadiness(t *testing.T) {
	t.Parallel()

	db := &crossScopeReadinessWiringDB{}
	logged := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logged, nil))
	service, err := buildReducerService(
		context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{},
		postgres.NewSharedIntentStore(db), stubCypherReader{}, stubCypherReader{},
		func(string) string { return "" }, nil, nil, logger, nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	_, execErr := service.Executor.Execute(context.Background(), reducer.Intent{
		IntentID:       "cross-scope-readiness-wiring",
		Domain:         reducer.DomainCICDRunCorrelation,
		ScopeID:        "scope-123",
		GenerationID:   "generation-456",
		SourceSystem:   "github_actions",
		CycleStartedAt: time.Now().UTC(),
		EnqueuedAt:     time.Now().UTC(),
	})
	if execErr == nil {
		t.Fatal("execution error = nil, want a readiness deferral: the floor is not wired into buildReducerService")
	}

	var classified interface{ FailureClass() string }
	if !errors.As(execErr, &classified) {
		t.Fatalf("execution error = %v, want an error carrying a failure class", execErr)
	}
	if got := classified.FailureClass(); got != reducer.CrossScopeProducerNotReadyFailureClass {
		t.Fatalf(
			"failure class = %q, want %q",
			got, reducer.CrossScopeProducerNotReadyFailureClass,
		)
	}
	if !db.probedProducerQuiescence {
		t.Fatal("the producer-quiescence probe never ran: the readiness store is not wired")
	}

	// The logger is wired separately and fails silently on its own. Without it
	// a deferral leaves no elapsed-versus-bound signal anywhere, because this
	// failure class freezes attempt_count and the queue row therefore cannot
	// say how long the consumer has been waiting.
	for _, want := range []string{
		"cross-scope consumer deferred",
		"elapsed_since_cycle_start",
		"max_wait",
	} {
		if !strings.Contains(logged.String(), want) {
			t.Fatalf("deferral log is missing %q; got:\n%s", want, logged.String())
		}
	}
}

// crossScopeReadinessWiringDB answers the handful of queries one
// ci_cd_run_correlation pass makes, with the two answers this test turns on:
// one ci.artifact fact carrying a digest (so the pass has something to look up,
// which is what arms the floor), and a producer scope that is registered but not
// quiescent (so the floor defers).
type crossScopeReadinessWiringDB struct {
	fakeReducerDB
	probedProducerQuiescence bool
}

func (f *crossScopeReadinessWiringDB) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	// The producer-scope quiescence probe. Identified by the projector-drain
	// fence, which no other reducer query carries.
	if strings.Contains(query, "FROM fact_work_items AS projector_work") {
		f.probedProducerQuiescence = true
		return &crossScopeReadinessRows{rows: [][]any{
			{"oci_registry:ghcr.io/acme", false},
		}}, nil
	}
	// The cross-scope identity read-back resolves nothing. Combined with the
	// not-quiescent probe answer above, that is the #5709 condition: an empty
	// join whose producer has not published yet.
	if strings.Contains(query, "container_image_identity_current_support_facts_for") {
		return &crossScopeReadinessRows{}, nil
	}
	if strings.Contains(query, "FROM fact_records") {
		return &crossScopeReadinessRows{rows: [][]any{
			crossScopeReadinessArtifactFactRow("scope-123", "generation-456"),
		}}, nil
	}
	return f.fakeReducerDB.QueryContext(ctx, query, args...)
}

// BeginReadOnlyRepeatableRead satisfies the snapshot seam the identity
// read-back requires. The transaction routes straight back to this fake.
func (f *crossScopeReadinessWiringDB) BeginReadOnlyRepeatableRead(
	context.Context,
) (postgres.Transaction, error) {
	return crossScopeReadinessTx{db: f}, nil
}

// crossScopeReadinessTx is a pass-through transaction over the fake database.
type crossScopeReadinessTx struct {
	db *crossScopeReadinessWiringDB
}

func (t crossScopeReadinessTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return t.db.ExecContext(ctx, query, args...)
}

func (t crossScopeReadinessTx) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (postgres.Rows, error) {
	return t.db.QueryContext(ctx, query, args...)
}

func (crossScopeReadinessTx) Commit() error { return nil }

func (crossScopeReadinessTx) Rollback() error { return nil }

// crossScopeReadinessArtifactFactRow builds one fact_records row in
// listFactsQuery column order for a ci.artifact fact carrying a digest.
func crossScopeReadinessArtifactFactRow(scopeID, generationID string) []any {
	payload, err := json.Marshal(map[string]any{
		"provider":        "github_actions",
		"run_id":          "run-1",
		"run_attempt":     "1",
		"artifact_type":   "container_image",
		"artifact_digest": "sha256:" + strings.Repeat("a", 64),
	})
	if err != nil {
		panic(err)
	}
	return []any{
		"fact-artifact-1",
		scopeID,
		generationID,
		facts.CICDArtifactFactKind,
		"github_actions/run-1/1/artifact",
		"",
		"ci_cd_run",
		int64(1),
		facts.SourceConfidenceReported,
		"github_actions",
		"github_actions/run-1/1/artifact",
		"",
		"",
		time.Now().UTC(),
		false,
		payload,
	}
}

// crossScopeReadinessRows is a minimal postgres.Rows over pre-built values.
type crossScopeReadinessRows struct {
	rows  [][]any
	index int
}

func (r *crossScopeReadinessRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *crossScopeReadinessRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	if len(dest) != len(row) {
		return errors.New("scan destination count does not match the fixture row width")
	}
	for i := range dest {
		if err := crossScopeReadinessAssign(dest[i], row[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *crossScopeReadinessRows) Err() error { return nil }

func (r *crossScopeReadinessRows) Close() error { return nil }

// crossScopeReadinessAssign copies one fixture value into one scan target.
func crossScopeReadinessAssign(dest, value any) error {
	switch target := dest.(type) {
	case *string:
		v, ok := value.(string)
		if !ok {
			return errors.New("fixture value is not a string")
		}
		*target = v
	case *bool:
		v, ok := value.(bool)
		if !ok {
			return errors.New("fixture value is not a bool")
		}
		*target = v
	case *int64:
		v, ok := value.(int64)
		if !ok {
			return errors.New("fixture value is not an int64")
		}
		*target = v
	case *time.Time:
		v, ok := value.(time.Time)
		if !ok {
			return errors.New("fixture value is not a time.Time")
		}
		*target = v
	case *[]byte:
		v, ok := value.([]byte)
		if !ok {
			return errors.New("fixture value is not a []byte")
		}
		*target = v
	default:
		return errors.New("unsupported scan target")
	}
	return nil
}
