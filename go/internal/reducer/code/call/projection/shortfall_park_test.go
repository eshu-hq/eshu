// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// retryableShortfallError scripts the writer's #10938 park error: a shortfall
// on an enforced relationship fails the write retryably under the shortfall
// failure class so the visit fails before anything completes. The class is
// what keys the #10947 bound; an unclassed retryable error never anchors it
// (see unclassedShortfallError in shortfall_bound_test.go).
type retryableShortfallError struct{ msg string }

func (e *retryableShortfallError) Error() string   { return e.msg }
func (e *retryableShortfallError) Retryable() bool { return true }

// FailureClass returns the shortfall failure class the production writer
// pins on codeCallMatchShortfallError (non-counting, so the park re-polls
// instead of dead-lettering on attempt budget).
func (e *retryableShortfallError) FailureClass() string { return codeCallShortfallFailureClass }

// shortfallCodeCallWriter scripts a #10938 shortfall: the write attempt is
// recorded, the report carries zero matches, and the write fails retryably.
type shortfallCodeCallWriter struct {
	writeCalls []recordedProjectionCall
	err        error
}

func (w *shortfallCodeCallWriter) RetractEdges(_ context.Context, _ string, rows []sharedintent.Row, evidenceSource string) error {
	return nil
}

func (w *shortfallCodeCallWriter) WriteEdges(_ context.Context, _ string, rows []sharedintent.Row, evidenceSource string) (sharedintent.WriteReport, error) {
	w.writeCalls = append(w.writeCalls, recordedProjectionCall{
		rows:           append([]sharedintent.Row(nil), rows...),
		evidenceSource: evidenceSource,
	})
	return sharedintent.WriteReport{SubmittedRows: len(rows)}, w.err
}

func shortfallTestRunner(writer sharedintent.EdgeWriter, reader *fakeCodeCallIntentStore) Runner {
	return Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}
}

// TestCodeCallProjectionShortfallFailsVisitBeforeCompletion is the #10938
// runner half of the GREEN regression: when the write reports a shortfall
// (the writer's park error), the visit fails and marks nothing completed —
// the intent stays open for the retry that binds after the callee phase
// publishes. The error path in processPartitionOnce returns before the
// MarkIntentsCompleted call, so a parked batch cannot complete silently.
func TestCodeCallProjectionShortfallFailsVisitBeforeCompletion(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 37, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-2", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-2", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	writer := &shortfallCodeCallWriter{err: &retryableShortfallError{msg: "code_calls CALLS batch matched 0 of 1 submitted rows"}}
	runner := shortfallTestRunner(writer, reader)

	_, err := runner.processOnce(context.Background(), now)
	if err == nil {
		t.Fatal("processOnce() with a shortfalled write = nil error, want the park error so the intent stays open")
	}
	if !reducercontract.IsRetryable(err) {
		t.Fatalf("processOnce() error = %v, want retryable so the visit re-polls", err)
	}
	if got := len(writer.writeCalls); got != 1 {
		t.Fatalf("write attempts = %d, want 1 (the batch did execute)", got)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %v, want none (parked batch completes nothing)", reader.marked)
	}
}

// TestCodeCallProjectionFullMatchReportsCounts pins the verified-write
// telemetry flow: a fully matched write completes the intent and carries the
// submitted/matched counts on the visit result (#10938).
func TestCodeCallProjectionFullMatchReportsCounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 37, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-2", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-2", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := shortfallTestRunner(writer, reader)

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := result.CodeCallSubmittedRows, 1; got != want {
		t.Fatalf("CodeCallSubmittedRows = %d, want %d", got, want)
	}
	if got, want := result.CodeCallMatchedRows, 1; got != want {
		t.Fatalf("CodeCallMatchedRows = %d, want %d", got, want)
	}
	if got := len(reader.marked); got != 1 {
		t.Fatalf("marked completed = %d intents, want 1", got)
	}
}
