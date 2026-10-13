// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package preparse

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type answerReprojector struct{ reason string }

func (r answerReprojector) ReprojectActiveGeneration(context.Context, scope.ActiveReprojection) (string, error) {
	return r.reason, nil
}

// TestRunEventNamesTheScope (review P3-a): the decision event carries the
// scope id, so the several decisions of one sync cycle are told apart in a
// trace, along with the outcome and whether the parse was skipped.
func TestRunEventNamesTheScope(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	ctx, span := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("t").Start(context.Background(), "cycle")
	attempt := Attempt{
		ScopeID: "git-repository-scope:big", Reason: ReasonIntervalElapsed, RemoteCommit: "c", Identity: "preparse:v1:x", MaxSkips: 7,
		State: scope.FullReconcileState{ActiveGenerationID: "gen-1", ActiveCommitSHA: "c", ActivePreparseIdentity: "preparse:v1:x"},
	}
	if !Run(ctx, attempt, func() (bool, error) { return true, nil }, answerReprojector{reason: telemetry.RefreshSkipReasonReconcileReprojected}, Recorder{}) {
		t.Fatal("Run() = false, want a skipped parse")
	}
	span.End()
	events := recorder.Ended()[0].Events()
	if len(events) != 1 || events[0].Name != "git.reconcile.parse_skip_decision" {
		t.Fatalf("events = %+v, want one git.reconcile.parse_skip_decision", events)
	}
	got := map[string]string{}
	for _, kv := range events[0].Attributes {
		got[string(kv.Key)] = kv.Value.String()
	}
	want := map[string]string{
		"scope_id": attempt.ScopeID, "eshu.reconcile.parse_skip_outcome": "skipped", "eshu.reconcile.parse_skipped": "true",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("event attribute %s = %q, want %q (all %v)", key, got[key], value, got)
		}
	}
}
