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

func (r answerReprojector) StampActiveGeneration(context.Context, scope.ActiveReprojection) (string, error) {
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

type spyReprojector struct {
	reason string
	got    scope.ActiveReprojection
	calls  int
	// stampGot and stampCalls record the stamp-path calls; got and calls
	// record the skip-path calls, so a test can assert which path Run took.
	stampGot   scope.ActiveReprojection
	stampCalls int
}

func (r *spyReprojector) ReprojectActiveGeneration(_ context.Context, request scope.ActiveReprojection) (string, error) {
	r.got = request
	r.calls++
	return r.reason, nil
}

func (r *spyReprojector) StampActiveGeneration(_ context.Context, request scope.ActiveReprojection) (string, error) {
	r.stampGot = request
	r.stampCalls++
	return r.reason, nil
}

// TestRunStampsIdentityWithoutParsing (#8095): with the opt-in setting and a
// proven generation, the first sweep re-projects the active generation with
// the computed identity and stores no new generation, so the stamped facts
// equal a full parse's facts by construction (no parse runs, no facts change).
func TestRunStampsIdentityWithoutParsing(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	ctx, span := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("t").Start(context.Background(), "cycle")
	spy := &spyReprojector{reason: telemetry.RefreshSkipReasonReconcileReprojected}
	attempt := Attempt{
		ScopeID: "git-repository-scope:first-sweep", Reason: ReasonIntervalElapsed,
		RemoteCommit: "c", Identity: "preparse:v1:now", MaxSkips: 7,
		StampWithoutParse: true, StampProven: true,
		State: scope.FullReconcileState{ActiveGenerationID: "gen-0", ActiveCommitSHA: "c"},
	}
	if !Run(ctx, attempt, func() (bool, error) { return true, nil }, spy, Recorder{}) {
		t.Fatal("Run() = false, want the stamp to skip the parse")
	}
	// The stamp takes the store stamp path, never the identity-equality
	// path: that fence would refuse it (stored empty never equals requested).
	if spy.stampCalls != 1 {
		t.Fatalf("stamp calls = %d, want 1", spy.stampCalls)
	}
	if spy.calls != 0 {
		t.Fatalf("skip-path calls = %d, want 0 (the stamp must not take the identity-equality path)", spy.calls)
	}
	if spy.stampGot.GenerationID != "gen-0" || spy.stampGot.CommitSHA != "c" || spy.stampGot.PreparseIdentity != "preparse:v1:now" {
		t.Fatalf("stamp request = %+v, want gen-0 at c with the computed identity", spy.stampGot)
	}
	span.End()
	events := recorder.Ended()[0].Events()
	if len(events) != 1 {
		t.Fatalf("events = %+v, want one git.reconcile.parse_skip_decision", events)
	}
	outcome := ""
	skipped := ""
	for _, kv := range events[0].Attributes {
		switch string(kv.Key) {
		case "eshu.reconcile.parse_skip_outcome":
			outcome = kv.Value.String()
		case "eshu.reconcile.parse_skipped":
			skipped = kv.Value.String()
		}
	}
	if outcome != "stamped_without_parse" || skipped != "true" {
		t.Fatalf("span outcome = %q skipped = %q, want stamped_without_parse/true", outcome, skipped)
	}
}

// TestRunParsesWhenTheStampIsUnproven (#8095): the setting without the proof
// parses exactly as before, so the default and the unproven opt-in sweep keep
// today's behavior and facts.
func TestRunParsesWhenTheStampIsUnproven(t *testing.T) {
	t.Parallel()
	for _, proven := range []bool{false, true} {
		spy := &spyReprojector{reason: telemetry.RefreshSkipReasonReconcileReprojected}
		attempt := Attempt{
			ScopeID: "git-repository-scope:first-sweep", Reason: ReasonIntervalElapsed,
			RemoteCommit: "c", Identity: "preparse:v1:now", MaxSkips: 7,
			StampWithoutParse: false, StampProven: proven,
			State: scope.FullReconcileState{ActiveGenerationID: "gen-0", ActiveCommitSHA: "c"},
		}
		if Run(context.Background(), attempt, func() (bool, error) { return true, nil }, spy, Recorder{}) {
			t.Fatalf("proven=%t: Run() = true, want a parse when the setting is off", proven)
		}
		if spy.calls != 0 {
			t.Fatalf("proven=%t: re-projection calls = %d, want 0", proven, spy.calls)
		}
	}
}
