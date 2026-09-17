// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// targetMissProbeExecutor is a fake Executor that also implements
// ProbeExecutor so tests can script the write-target existence probe.
// probeAllPresent scripts the presence probe: false means at least one batch
// row's graph target is absent, so the batch must not complete silently.
type targetMissProbeExecutor struct {
	executeCalls    int
	probeCalls      int
	probeStmts      []Statement
	probeAllPresent bool
	probeErr        error
}

func (e *targetMissProbeExecutor) Execute(_ context.Context, _ Statement) error {
	e.executeCalls++
	return nil
}

func (e *targetMissProbeExecutor) ExecuteProbe(_ context.Context, stmt Statement) (bool, error) {
	e.probeCalls++
	e.probeStmts = append(e.probeStmts, stmt)
	if e.probeErr != nil {
		return false, e.probeErr
	}
	return e.probeAllPresent, nil
}

func validHandlesRouteRow() reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"function_entity_id": "content-entity:gw",
			"repo_id":            "repo-a",
			"path":               "/widgets",
			"http_method":        "GET",
			"framework":          "express",
			"resolution_method":  "same_file",
			"confidence":         0.95,
			"reason":             "test",
		},
	}
}

func validRunsInRow() reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload: map[string]any{
			"function_id":       "content-entity:gw",
			"repo_id":           "repo-a",
			"resolution_method": "same_file",
			"confidence":        0.5,
			"ambiguous":         true,
		},
	}
}

// TestEdgeWriterWriteEdgesHandlesRouteAbsentTargetFailsClosed is the #6184
// interrupted-rebuild regression: a HANDLES_ROUTE batch whose endpoint target
// is absent from the graph must fail retryably instead of completing with a
// silent zero-edge write. The presence gate proves the endpoint committed in
// Postgres, but a rebuild that wiped the graph (or a materialization that has
// not recommitted it yet) leaves the MATCH with nothing to bind; completing
// that batch loses the edge with no error and no dead letter.
func TestEdgeWriterWriteEdgesHandlesRouteAbsentTargetFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: false}
	writer := NewEdgeWriter(executor, 0)

	_, err := writer.WriteEdges(
		context.Background(), reducer.DomainHandlesRoute,
		[]reducer.SharedProjectionIntentRow{validHandlesRouteRow()},
		"parser/framework-routes",
	)
	if err == nil {
		t.Fatal("WriteEdges() with an absent endpoint target succeeded silently, want a retryable error so the batch re-runs after materialization recommits the endpoint")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want a retryable error so the rows stay queued", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("executor writes = %d, want 0: no edge statement may run when a batch target is absent", executor.executeCalls)
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
}

// TestEdgeWriterWriteEdgesRunsInAbsentTargetFailsClosed is the RUNS_IN half
// of the same #6184 regression: a batch whose repo defines no workload in the
// graph yet must fail retryably rather than complete a silent zero-edge
// write.
func TestEdgeWriterWriteEdgesRunsInAbsentTargetFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: false}
	writer := NewEdgeWriter(executor, 0)

	_, err := writer.WriteEdges(
		context.Background(), reducer.DomainRunsIn,
		[]reducer.SharedProjectionIntentRow{validRunsInRow()},
		"reducer/runs-in",
	)
	if err == nil {
		t.Fatal("WriteEdges() with no workload defined for the repo succeeded silently, want a retryable error so the batch re-runs after materialization commits the workload")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want a retryable error so the rows stay queued", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("executor writes = %d, want 0: no edge statement may run when a batch target is absent", executor.executeCalls)
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
}

// TestEdgeWriterWriteEdgesTargetPresentProceeds guards the happy path: when
// every batch target exists, the probe must not cost the write anything.
func TestEdgeWriterWriteEdgesTargetPresentProceeds(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: true}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(
		context.Background(), reducer.DomainHandlesRoute,
		[]reducer.SharedProjectionIntentRow{validHandlesRouteRow()},
		"parser/framework-routes",
	); err != nil {
		t.Fatalf("WriteEdges() with all targets present error = %v", err)
	}
	if executor.executeCalls == 0 {
		t.Fatal("expected the edge write to run when all targets are present")
	}
}

// TestEdgeWriterWriteEdgesProbeErrorFailsClosed pins the #6730 Codex P1
// direction: a failing existence probe must defer the batch retryably, never
// write unchecked. Fail-open recreates the exact silent edge loss the guard
// exists to prevent whenever the probe shape fails while a target is
// actually absent (timeout or rejection under load); the rows stay queued on
// the same non-counting retry class as a detected miss and re-run after the
// backend recovers.
func TestEdgeWriterWriteEdgesProbeErrorFailsClosed(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeErr: errors.New("probe backend unavailable")}
	writer := NewEdgeWriter(executor, 0)

	_, err := writer.WriteEdges(
		context.Background(), reducer.DomainHandlesRoute,
		[]reducer.SharedProjectionIntentRow{validHandlesRouteRow()},
		"parser/framework-routes",
	)
	if err == nil {
		t.Fatal("WriteEdges() with a failing probe succeeded silently, want a retryable error so the unverified batch re-runs instead of risking a silent zero-edge write")
	}
	if !reducer.IsRetryable(err) {
		t.Fatalf("WriteEdges() error = %v, want a retryable error so the rows stay queued", err)
	}
	if executor.executeCalls != 0 {
		t.Fatalf("executor writes = %d, want 0: no edge statement may run when the probe cannot verify its targets", executor.executeCalls)
	}
	if executor.probeCalls == 0 {
		t.Fatal("target existence probe was never consulted")
	}
}

// TestEdgeWriterWriteEdgesWithoutProbeCapabilityWrites pins the unwired
// executor path: without a ProbeExecutor the writer keeps today's behavior
// byte-identical instead of failing every batch it cannot verify.
func TestEdgeWriterWriteEdgesWithoutProbeCapabilityWrites(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(
		context.Background(), reducer.DomainHandlesRoute,
		[]reducer.SharedProjectionIntentRow{validHandlesRouteRow()},
		"parser/framework-routes",
	); err != nil {
		t.Fatalf("WriteEdges() without probe capability error = %v", err)
	}
	if got, want := len(executor.calls), 1; got != want {
		t.Fatalf("executor calls = %d, want %d", got, want)
	}
}

// TestEdgeWriterTargetMissProbeMentionsBatchScope pins the probe shape the
// guard relies on: the miss-detection statement must carry the batch rows so
// the backend checks exactly this batch's targets, and must be read-only.
func TestEdgeWriterTargetMissProbeMentionsBatchScope(t *testing.T) {
	t.Parallel()

	executor := &targetMissProbeExecutor{probeAllPresent: true}
	writer := NewEdgeWriter(executor, 0)

	if _, err := writer.WriteEdges(
		context.Background(), reducer.DomainHandlesRoute,
		[]reducer.SharedProjectionIntentRow{validHandlesRouteRow()},
		"parser/framework-routes",
	); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if len(executor.probeStmts) == 0 {
		t.Fatal("target existence probe was never consulted")
	}
	probe := executor.probeStmts[0].Cypher
	for _, want := range []string{"MATCH (e0:Endpoint", "RETURN 1 LIMIT 1"} {
		if !strings.Contains(probe, want) {
			t.Fatalf("probe cypher missing %q:\n%s", want, probe)
		}
	}
	for _, forbidden := range []string{"MERGE", "CREATE", "DELETE", "SET ", "OPTIONAL", "WITH", "WHERE", "COUNT", "UNWIND"} {
		if strings.Contains(probe, forbidden) {
			t.Fatalf("probe cypher must be anchored MATCHes only, found %q:\n%s", forbidden, probe)
		}
	}
}

// TestEdgeWriterHandlesRouteProbeAnchorsFunction pins the #6730 owner
// finding: the HANDLES_ROUTE write MATCHes two nodes — (f:Function {uid})
// and (e:Endpoint {repo_id, path}) — so the presence probe must anchor both.
// An Endpoint-only probe lets a batch whose Function node is absent (post-wipe
// ordering vs content projection) pass the guard and complete a silent
// zero-edge write.
func TestEdgeWriterHandlesRouteProbeAnchorsFunction(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{{
		"function_entity_id": "content-entity:gw",
		"repo_id":            "repo-a",
		"path":               "/widgets",
	}}
	probe, ok := buildTargetPresenceProbeStatement(reducer.DomainHandlesRoute, rows)
	if !ok {
		t.Fatal("buildTargetPresenceProbeStatement(handles_route) = false, want a probe anchoring Function and Endpoint")
	}
	for _, want := range []string{":Function {uid: $", ":Endpoint {repo_id: $", "RETURN 1 LIMIT 1"} {
		if !strings.Contains(probe.Cypher, want) {
			t.Fatalf("probe cypher missing %q:\n%s", want, probe.Cypher)
		}
	}
	found := map[string]bool{}
	for _, v := range probe.Parameters {
		if s, ok := v.(string); ok {
			found[s] = true
		}
	}
	for _, want := range []string{"content-entity:gw", "repo-a", "/widgets"} {
		if !found[want] {
			t.Fatalf("probe parameters missing target %q: %v", want, probe.Parameters)
		}
	}
}
