// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graphbackpressure"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// reducerNeo4jExecutorSatisfiesProbeExecutor is a compile-time assertion that
// the production adapter type carries the #5998 ProbeExecutor capability, so
// a future edit that removes reducerNeo4jExecutor.ExecuteProbe fails the build
// rather than silently regressing the probe guard to permanently inert.
var _ sourcecypher.ProbeExecutor = reducerNeo4jExecutor{}

// runCypherOnlySession implements cypherRunner (RunCypher/RunCypherGroup)
// only, deliberately WITHOUT QueryCypherExists, so tests can prove
// cypherRunnerStatementExecutor.ExecuteProbe fails safe (an error, not a
// silent "not found") when the wrapped runner cannot answer a probe. It does
// not embed *fakeNeo4jSession: Go promotes an embedded pointer's methods,
// which would silently satisfy cypherProber via promotion and defeat the
// point of this fake (mirrors faultExecuteOnlyRecordingExecutor in
// internal/storage/cypher/fault_executor_test.go).
type runCypherOnlySession struct {
	calls []fakeCypherCall
}

func (s *runCypherOnlySession) RunCypher(_ context.Context, cypher string, params map[string]any) error {
	s.calls = append(s.calls, fakeCypherCall{Cypher: cypher, Parameters: params})
	return nil
}

func (s *runCypherOnlySession) RunCypherGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	for _, stmt := range stmts {
		if err := s.RunCypher(ctx, stmt.Cypher, stmt.Parameters); err != nil {
			return err
		}
	}
	return nil
}

// TestReducerNeo4jExecutorExecuteProbeForwardsToRunnerQueryCypherExists proves
// reducerNeo4jExecutor.ExecuteProbe reaches the wrapped session's
// QueryCypherExists through the persistent RetryingExecutor seam, for both a
// found and a not-found result.
func TestReducerNeo4jExecutorExecuteProbeForwardsToRunnerQueryCypherExists(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		t.Parallel()
		session := &fakeNeo4jSession{probeFound: true}
		executor := newReducerNeo4jExecutor(session, nil)

		found, err := executor.ExecuteProbe(context.Background(), sourcecypher.Statement{
			Cypher:     "MATCH (r:Rationale)-[rel:EXPLAINS]->() RETURN rel LIMIT 1",
			Parameters: map[string]any{"repo_ids": []string{"repo-a"}},
		})
		if err != nil {
			t.Fatalf("ExecuteProbe() error = %v, want nil", err)
		}
		if !found {
			t.Fatal("ExecuteProbe() found = false, want true")
		}
		if got := len(session.probeCalls); got != 1 {
			t.Fatalf("session.probeCalls = %d, want 1", got)
		}
		if got := len(session.calls); got != 0 {
			t.Fatalf("session.calls (RunCypher/DELETE) = %d, want 0 (probe must not run RunCypher)", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		session := &fakeNeo4jSession{probeFound: false}
		executor := newReducerNeo4jExecutor(session, nil)

		found, err := executor.ExecuteProbe(context.Background(), sourcecypher.Statement{
			Cypher: "MATCH (r:Rationale)-[rel:EXPLAINS]->() RETURN rel LIMIT 1",
		})
		if err != nil {
			t.Fatalf("ExecuteProbe() error = %v, want nil", err)
		}
		if found {
			t.Fatal("ExecuteProbe() found = true, want false")
		}
		if got := len(session.probeCalls); got != 1 {
			t.Fatalf("session.probeCalls = %d, want 1", got)
		}
	})
}

// TestCypherRunnerStatementExecutorExecuteProbeErrorsWhenRunnerLacksQueryCypherExists
// proves the fail-safe direction at the innermost adapter seam: when the
// wrapped cypherRunner does not implement cypherProber, ExecuteProbe returns
// an error (never a silent "not found") so the caller's fail-safe guard runs
// the DELETE unconditionally rather than skipping it on an unknown answer.
func TestCypherRunnerStatementExecutorExecuteProbeErrorsWhenRunnerLacksQueryCypherExists(t *testing.T) {
	t.Parallel()

	session := &runCypherOnlySession{}
	executor := cypherRunnerStatementExecutor{runner: session}

	found, err := executor.ExecuteProbe(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1 LIMIT 1"})
	if err == nil {
		t.Fatal("ExecuteProbe() error = nil, want non-nil (runner lacks QueryCypherExists)")
	}
	if found {
		t.Fatal("ExecuteProbe() found = true, want false on the unsupported path")
	}
}

// TestReducerNeo4jExecutorExecuteProbePropagatesQueryCypherExistsError proves a
// probe error at the session layer surfaces unchanged through
// reducerNeo4jExecutor, so a caller's fail-safe guard (never silently skip)
// sees the failure and runs the DELETE unconditionally.
func TestReducerNeo4jExecutorExecuteProbePropagatesQueryCypherExistsError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("neo4j read session unavailable")
	session := &fakeNeo4jSession{probeErr: wantErr}
	executor := newReducerNeo4jExecutor(session, nil)

	found, err := executor.ExecuteProbe(context.Background(), sourcecypher.Statement{Cypher: "RETURN 1 LIMIT 1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExecuteProbe() error = %v, want %v", err, wantErr)
	}
	if found {
		t.Fatal("ExecuteProbe() found = true, want false on a probe error")
	}
}

// gateWiredEdgeWriterForProbeTest builds the REAL production graph-write
// chain, not an approximation of it: InstrumentedExecutor wraps
// reducerNeo4jExecutor, exactly as go/cmd/reducer/observed_service_wiring.go
// does at line 42-46 (`instrumentedNeo4j := &sourcecypher.InstrumentedExecutor{
// Inner: neo4jExecutor, ...}`) BEFORE that value is ever passed into
// buildReducerService, and only THEN does reducerGraphWriteGate.boundExecutor
// apply its two BackpressureExecutor layers on top -- the same order
// buildReducerService's own `neo4jExec = graphWriteGate.boundExecutor(neo4jExec)`
// produces when neo4jExec arrives pre-wrapped in InstrumentedExecutor. An
// earlier version of this helper wrapped reducerNeo4jExecutor directly with
// the gate and OMITTED InstrumentedExecutor, which made this test's own
// `wrapped.(sourcecypher.ProbeExecutor)` check a false green (review F1):
// *BackpressureExecutor always implements ExecuteProbe regardless of what its
// inner wraps, so the type assertion here can never by itself detect a
// capability silently dying at a middle layer -- only a full pass all the way
// through to the session, as RetractEdges below performs, can.
func gateWiredEdgeWriterForProbeTest(t *testing.T, probeFound bool) (*sourcecypher.EdgeWriter, *fakeNeo4jSession) {
	t.Helper()

	session := &fakeNeo4jSession{probeFound: probeFound}
	base := newReducerNeo4jExecutor(session, nil)
	instrumented := &sourcecypher.InstrumentedExecutor{Inner: base}

	getenv := func(key string) string {
		if key == graphbackpressure.MaxInFlightEnv {
			return "4"
		}
		return ""
	}
	gate := newReducerGraphWriteGate(getenv, nil)
	wrapped := gate.boundExecutor(instrumented)

	if _, ok := wrapped.(sourcecypher.ProbeExecutor); !ok {
		t.Fatalf("gate-wrapped reducerNeo4jExecutor chain (%T) does not satisfy sourcecypher.ProbeExecutor -- the #5998 probe guard has regressed to permanently inert in production", wrapped)
	}

	return sourcecypher.NewEdgeWriter(wrapped, 0), session
}

// TestReducerGraphWriteGateWiredChainSkipsRationaleDeleteOnEmptyProbe is the
// end-to-end proof that Change 2 actually lands the capability in production:
// with the exact reducerGraphWriteGate.boundExecutor(reducerNeo4jExecutor)
// composition buildReducerService wires (backpressure active, so the chain is
// two BackpressureExecutor layers deep), a zero-result probe still reaches the
// session's QueryCypherExists and the paired DELETE (RunCypher) never fires.
// This test would fail (probe silently "unsupported", DELETE always runs) if
// either forward in Change 2 were missing or reverted.
func TestReducerGraphWriteGateWiredChainSkipsRationaleDeleteOnEmptyProbe(t *testing.T) {
	t.Parallel()

	writer, session := gateWiredEdgeWriterForProbeTest(t, false)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      map[string]any{"repo_id": "repo-a"},
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(session.probeCalls); got != 1 {
		t.Fatalf("session.probeCalls = %d, want 1 (the probe must reach the session through the full gate-wrapped chain)", got)
	}
	if got := len(session.calls); got != 0 {
		t.Fatalf("session.calls (RunCypher/DELETE) = %d, want 0 (probe found nothing, delete must be skipped)", got)
	}
}

// TestReducerGraphWriteGateWiredChainRunsRationaleDeleteWhenProbeFindsRows is
// the symmetric case: through the same gate-wrapped production chain, a probe
// that finds rows still lets the paired DELETE run.
func TestReducerGraphWriteGateWiredChainRunsRationaleDeleteWhenProbeFindsRows(t *testing.T) {
	t.Parallel()

	writer, session := gateWiredEdgeWriterForProbeTest(t, true)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      map[string]any{"repo_id": "repo-a"},
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(session.probeCalls); got != 1 {
		t.Fatalf("session.probeCalls = %d, want 1", got)
	}
	if got := len(session.calls); got != 1 {
		t.Fatalf("session.calls (RunCypher/DELETE) = %d, want 1 (probe found rows, delete must run)", got)
	}
}
