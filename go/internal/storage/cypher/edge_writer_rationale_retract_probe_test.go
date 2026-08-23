// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestBuildProbeRationaleEdgesMirrorsRetractShape guards the #5998 probe-guard
// contract: the probe statement MUST use the identical MATCH/WHERE shape and
// parameters as BuildRetractRationaleEdges for the same inputs, for both the
// multi-source (canonical-plus-legacy) and single-source (custom evidence
// source) branches, differing only in the terminal clause. A probe that asks a
// different question than the delete it guards is a correctness bug. It also
// asserts the two Statements carry DIFFERENT Operation values: Operation feeds
// BackpressureGate.Acquire's wait-metric label, and this guard runs one probe
// per RetractEdges batch whether or not the paired DELETE follows, so a probe
// sharing the retract's OperationCanonicalRetract label would count every probe
// as a retract and mask the ratio of probes to real deletes (review finding).
// The retry counter is not in play: ExecuteProbe does not retry.
func TestBuildProbeRationaleEdgesMirrorsRetractShape(t *testing.T) {
	t.Parallel()

	t.Run("multi-source canonical", func(t *testing.T) {
		t.Parallel()
		repoIDs := []string{"repo-a", "repo-b"}
		retract := BuildRetractRationaleEdges(repoIDs, canonicalRationaleEvidenceSource)
		probe := BuildProbeRationaleEdges(repoIDs, canonicalRationaleEvidenceSource)

		if probe.Cypher != probeCanonicalRationaleEdgesCypher {
			t.Fatalf("probe cypher = %q, want probeCanonicalRationaleEdgesCypher", probe.Cypher)
		}
		// Compare EVERYTHING up to the terminal clause, not just the first two
		// lines (review F3): a SplitN(cypher, "\n", 3)[:2] comparison discards
		// the "AND rel.evidence_source ..." line -- the one line that differs
		// between the single-source and multi-source shapes, and therefore the
		// likeliest place for the probe and retract to silently drift apart.
		wantProbeCypher := strings.TrimSuffix(retract.Cypher, "DELETE rel") + "RETURN true LIMIT 1"
		if probe.Cypher != wantProbeCypher {
			t.Fatalf("probe cypher diverged from retract cypher up to the terminal clause:\nretract %q\nprobe   %q\nwant    %q", retract.Cypher, probe.Cypher, wantProbeCypher)
		}
		if !reflect.DeepEqual(retract.Parameters, probe.Parameters) {
			t.Fatalf("parameters diverged:\nretract %#v\nprobe   %#v", retract.Parameters, probe.Parameters)
		}
		if retract.Operation != OperationCanonicalRetract {
			t.Fatalf("retract.Operation = %q, want OperationCanonicalRetract", retract.Operation)
		}
		if probe.Operation != OperationCanonicalProbe {
			t.Fatalf("probe.Operation = %q, want OperationCanonicalProbe", probe.Operation)
		}
		if probe.Operation == retract.Operation {
			t.Fatalf("probe and retract share Operation %q, want distinct values so backpressure/retry labelling cannot conflate probes with deletes", probe.Operation)
		}
	})

	t.Run("single-source custom", func(t *testing.T) {
		t.Parallel()
		repoIDs := []string{"repo-a"}
		const source = "custom/rationale"
		retract := BuildRetractRationaleEdges(repoIDs, source)
		probe := BuildProbeRationaleEdges(repoIDs, source)

		if probe.Cypher != probeRationaleEdgesCypher {
			t.Fatalf("probe cypher = %q, want probeRationaleEdgesCypher", probe.Cypher)
		}
		// See the "multi-source canonical" subtest above for why this compares
		// the full prefix rather than only the first two lines (review F3).
		wantProbeCypher := strings.TrimSuffix(retract.Cypher, "DELETE rel") + "RETURN true LIMIT 1"
		if probe.Cypher != wantProbeCypher {
			t.Fatalf("probe cypher diverged from retract cypher up to the terminal clause:\nretract %q\nprobe   %q\nwant    %q", retract.Cypher, probe.Cypher, wantProbeCypher)
		}
		if !reflect.DeepEqual(retract.Parameters, probe.Parameters) {
			t.Fatalf("parameters diverged:\nretract %#v\nprobe   %#v", retract.Parameters, probe.Parameters)
		}
		if retract.Operation != OperationCanonicalRetract {
			t.Fatalf("retract.Operation = %q, want OperationCanonicalRetract", retract.Operation)
		}
		if probe.Operation != OperationCanonicalProbe {
			t.Fatalf("probe.Operation = %q, want OperationCanonicalProbe", probe.Operation)
		}
		if probe.Operation == retract.Operation {
			t.Fatalf("probe and retract share Operation %q, want distinct values so backpressure/retry labelling cannot conflate probes with deletes", probe.Operation)
		}
	})
}

// probeGuardRecordingExecutor implements Executor and ProbeExecutor for
// testing the #5998 rationale retract probe-then-delete guard. It records
// every Execute (DELETE) and ExecuteProbe call so a test can assert whether
// the DELETE ran and with what statement.
type probeGuardRecordingExecutor struct {
	executeCalls []Statement
	probeCalls   []Statement
	probeFound   bool
	probeErr     error
}

func (e *probeGuardRecordingExecutor) Execute(_ context.Context, stmt Statement) error {
	e.executeCalls = append(e.executeCalls, stmt)
	return nil
}

func (e *probeGuardRecordingExecutor) ExecuteProbe(_ context.Context, stmt Statement) (bool, error) {
	e.probeCalls = append(e.probeCalls, stmt)
	return e.probeFound, e.probeErr
}

// TestEdgeWriterRetractEdgesRationaleProbeSkipsDeleteWhenProbeFindsNothing is
// the core #5998 regression: when the executor implements ProbeExecutor and
// the probe finds zero rows, RetractEdges MUST NOT run the expensive DELETE.
func TestEdgeWriterRetractEdgesRationaleProbeSkipsDeleteWhenProbeFindsNothing(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: false}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      wholeScopeRefreshPayload("repo-a"),
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(executor.probeCalls); got != 1 {
		t.Fatalf("probe calls = %d, want 1", got)
	}
	if got := len(executor.executeCalls); got != 0 {
		t.Fatalf("DELETE calls = %d, want 0 (probe found nothing, delete must be skipped)", got)
	}
	// The probe must have asked about repo-a specifically. Counting calls
	// cannot tell "probed repo-a and found nothing" from "probed an empty
	// repo_ids list", and the second would skip the DELETE for the wrong
	// reason (#6166).
	assertBoundRepoIDs(t, executor.probeCalls, []string{"repo-a"})
}

// TestEdgeWriterRetractEdgesRationaleProbeRunsDeleteWhenProbeFindsRows proves
// the guard runs the DELETE when the probe reports at least one matching row.
// This test MUST go RED if a bug skips the delete while edges exist.
func TestEdgeWriterRetractEdgesRationaleProbeRunsDeleteWhenProbeFindsRows(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      wholeScopeRefreshPayload("repo-a"),
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(executor.probeCalls); got != 1 {
		t.Fatalf("probe calls = %d, want 1", got)
	}
	if got := len(executor.executeCalls); got != 1 {
		t.Fatalf("DELETE calls = %d, want 1 (probe found rows, delete must run)", got)
	}
	if !strings.Contains(executor.executeCalls[0].Cypher, "DELETE rel") {
		t.Fatalf("executed statement = %q, want the DELETE retract", executor.executeCalls[0].Cypher)
	}
	// The Cypher assertion above is true even when repo_ids is empty, so the
	// binding is what proves the delete would actually remove repo-a's edges
	// rather than running over nothing (#6166).
	assertBoundRepoIDs(t, executor.executeCalls, []string{"repo-a"})
}

// TestEdgeWriterRetractEdgesRationaleProbeUnsupportedRunsDeleteUnconditionally
// proves the fail-safe direction: when the executor does not implement
// ProbeExecutor, RetractEdges runs the DELETE without attempting a probe at
// all, exactly as before the probe guard existed.
func TestEdgeWriterRetractEdgesRationaleProbeUnsupportedRunsDeleteUnconditionally(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{} // implements Executor only, not ProbeExecutor
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      wholeScopeRefreshPayload("repo-a"),
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(executor.calls); got != 1 {
		t.Fatalf("DELETE calls = %d, want 1 (unsupported executor must fail safe to unconditional delete)", got)
	}
	if !strings.Contains(executor.calls[0].Cypher, "DELETE rel") {
		t.Fatalf("executed statement = %q, want the DELETE retract", executor.calls[0].Cypher)
	}
	assertBoundRepoIDs(t, executor.calls, []string{"repo-a"})
}

// TestEdgeWriterRetractEdgesRationaleProbeErrorRunsDeleteUnconditionally
// proves the fail-safe direction for a probe that itself errors: the DELETE
// still runs, treating "probe failed" as "unknown", never "zero rows".
func TestEdgeWriterRetractEdgesRationaleProbeErrorRunsDeleteUnconditionally(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeErr: errors.New("probe backend unavailable")}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{{
		IntentID:     "i1",
		RepositoryID: "repo-a",
		Payload:      wholeScopeRefreshPayload("repo-a"),
	}}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if got := len(executor.probeCalls); got != 1 {
		t.Fatalf("probe calls = %d, want 1", got)
	}
	if got := len(executor.executeCalls); got != 1 {
		t.Fatalf("DELETE calls = %d, want 1 (probe error must fail safe to unconditional delete)", got)
	}
	assertBoundRepoIDs(t, executor.executeCalls, []string{"repo-a"})
}

// TestEdgeWriterRetractEdgesRationaleProbeUsesSameParametersAsDelete proves
// the probe statement mirrors the delete statement's shape and parameters for
// the exact call RetractEdges makes end to end, not just at the builder level
// (TestBuildProbeRationaleEdgesMirrorsRetractShape covers the builder
// directly). A probe that asks a different question than the delete it guards
// is a correctness bug.
func TestEdgeWriterRetractEdgesRationaleProbeUsesSameParametersAsDelete(t *testing.T) {
	t.Parallel()

	executor := &probeGuardRecordingExecutor{probeFound: true}
	writer := NewEdgeWriter(executor, 0)
	rows := []reducer.SharedProjectionIntentRow{
		{IntentID: "i1", RepositoryID: "repo-a", Payload: wholeScopeRefreshPayload("repo-a")},
	}

	if err := writer.RetractEdges(context.Background(), reducer.DomainRationaleEdges, rows, "reducer/rationale"); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if len(executor.probeCalls) != 1 || len(executor.executeCalls) != 1 {
		t.Fatalf("probe calls = %d, execute calls = %d, want 1 and 1", len(executor.probeCalls), len(executor.executeCalls))
	}
	probeStmt := executor.probeCalls[0]
	deleteStmt := executor.executeCalls[0]
	if !reflect.DeepEqual(probeStmt.Parameters, deleteStmt.Parameters) {
		t.Fatalf("probe parameters %#v diverged from delete parameters %#v", probeStmt.Parameters, deleteStmt.Parameters)
	}
	if probeStmt.Operation == deleteStmt.Operation {
		t.Fatalf("probe and delete share Operation %q end to end, want distinct values (review finding: backpressure/retry labelling must not conflate probes with deletes)", probeStmt.Operation)
	}
	if deleteStmt.Operation != OperationCanonicalRetract {
		t.Fatalf("delete Operation = %q, want OperationCanonicalRetract", deleteStmt.Operation)
	}
	if probeStmt.Operation != OperationCanonicalProbe {
		t.Fatalf("probe Operation = %q, want OperationCanonicalProbe", probeStmt.Operation)
	}
}
