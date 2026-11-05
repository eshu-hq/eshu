// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var testCommitRefusal = CommitRefusal{Reason: "destructive_successor_refused", Rule: "collector_ratio"}

// refusalObservingSource records both observer hooks, so a test can prove
// which one a commit disposition routes to.
type refusalObservingSource struct {
	*stubClaimedSourceWithObserver
	refusedCalls int
	refused      CommitRefusal
	refusedItem  workflow.WorkItem
	refusedErr   error
}

func (s *refusalObservingSource) ObserveClaimedGenerationRefused(
	_ context.Context,
	item workflow.WorkItem,
	refusal CommitRefusal,
) error {
	s.refusedCalls++
	s.refused = refusal
	s.refusedItem = item
	return s.refusedErr
}

// markingCommitter drains the stream and applies mark on the commit context,
// on whichever commit dispatch ClaimedService picks, the way IngestionStore
// marks the box from inside its one commit path.
type markingCommitter struct {
	stubClaimedCommitter
	mark func(context.Context)
	err  error
}

func newMarkingCommitter(mark func(context.Context), err error) *markingCommitter {
	committer := &markingCommitter{mark: mark, err: err}
	committer.claimedCommit = func(ctx context.Context, _ workflow.ClaimMutation, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope) error {
		return committer.finish(ctx, stream)
	}
	committer.claimedCommitWithStreamError = func(ctx context.Context, _ workflow.ClaimMutation, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope, _ func() error) error {
		return committer.finish(ctx, stream)
	}
	return committer
}

func (c *markingCommitter) finish(ctx context.Context, stream <-chan facts.Envelope) error {
	for range stream {
	}
	c.mark(ctx)
	return c.err
}

func (c *markingCommitter) CommitClaimedScopeGenerationWithState(
	ctx context.Context,
	_ workflow.ClaimMutation,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	stream <-chan facts.Envelope,
	_ func() error,
	_ GenerationStateWriter,
) error {
	return c.finish(ctx, stream)
}

type refusalClaimRun struct {
	store *stubClaimStore
	rm    metricdata.ResourceMetrics
	spans *tracetest.SpanRecorder
	err   error
}

// runRefusalClaim drives one real ClaimedService cycle and returns what it did
// to the claim and its telemetry.
func runRefusalClaim(t *testing.T, source ClaimedSource, committer Committer) refusalClaimRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	item := testClaimedWorkItem(now)
	item.SourceSystem = "git"
	claim := testWorkflowClaim(item.WorkItemID, now)
	store := &stubClaimStore{item: item, claim: claim, found: true}
	release := func(context.Context, workflow.ClaimMutation) error {
		cancel()
		return nil
	}
	store.heartbeat = release
	store.retryableFail = release
	store.terminalFail = release
	service := testClaimedService(now, claim, scope.CollectorGit, store, source, committer)
	instruments, reader := newTestInstrumentsPair(t)
	service.Instruments = instruments
	spans := tracetest.NewSpanRecorder()
	service.Tracer = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)).Tracer("test")

	err := service.Run(ctx)
	var rm metricdata.ResourceMetrics
	if collectErr := reader.Collect(context.Background(), &rm); collectErr != nil {
		t.Fatalf("Collect() error = %v", collectErr)
	}
	return refusalClaimRun{store: store, rm: rm, spans: spans, err: err}
}

func refusalTestSource(t *testing.T, stream bool, state bool) *refusalObservingSource {
	t.Helper()
	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	collected := successCollectedGeneration(t, now)
	if stream {
		collected.FactStreamErr = func() error { return nil }
	}
	if state {
		collected.StateWriter = func(context.Context, CommitExecutor) error { return nil }
	}
	return &refusalObservingSource{
		stubClaimedSourceWithObserver: &stubClaimedSourceWithObserver{
			stubClaimedSource: &stubClaimedSource{collected: collected, ok: true},
		},
	}
}

func metricRecorded(rm metricdata.ResourceMetrics, name string) bool {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

// TestClaimedServiceRoutesTheCommitDisposition is the #10037 table: a refused
// commit fires only the refusal observer, records no fact volume, and reports
// outcome refused, while the claim still completes; a committed or
// unchanged-skip commit behaves exactly as it did before. Every commit dispatch
// ClaimedService picks carries the box.
func TestClaimedServiceRoutesTheCommitDisposition(t *testing.T) {
	t.Parallel()

	dispositions := []struct {
		name          string
		mark          func(context.Context)
		wantCommitted int
		wantRefused   int
		wantFacts     int64
		wantOutcome   string
	}{
		{
			name:          "committed",
			mark:          func(context.Context) {},
			wantCommitted: 1,
			wantFacts:     1,
			wantOutcome:   telemetry.CollectorRunOutcomeSuccess,
		},
		{
			name:          "unchanged skip",
			mark:          MarkCommitUnchangedSkip,
			wantCommitted: 1,
			wantFacts:     1,
			wantOutcome:   telemetry.CollectorRunOutcomeSuccess,
		},
		{
			name:        "refused",
			mark:        func(ctx context.Context) { MarkCommitRefused(ctx, testCommitRefusal) },
			wantRefused: 1,
			wantOutcome: telemetry.CollectorRunOutcomeRefused,
		},
	}
	dispatches := []struct {
		name   string
		stream bool
		state  bool
	}{
		{name: "plain"},
		{name: "stream error", stream: true},
		{name: "generation state", state: true},
	}
	for _, disposition := range dispositions {
		for _, dispatch := range dispatches {
			t.Run(disposition.name+"/"+dispatch.name, func(t *testing.T) {
				t.Parallel()

				source := refusalTestSource(t, dispatch.stream, dispatch.state)
				run := runRefusalClaim(t, source, newMarkingCommitter(disposition.mark, nil))

				if run.err != nil {
					t.Fatalf("Run() error = %v, want nil: a refusal does not fail the claim", run.err)
				}
				if got := run.store.completeCalls; got != 1 {
					t.Fatalf("complete calls = %d, want 1", got)
				}
				if run.store.retryableFailCalls+run.store.terminalFailCalls != 0 {
					t.Fatalf("claim failed (retryable=%d terminal=%d), want completed",
						run.store.retryableFailCalls, run.store.terminalFailCalls)
				}
				if got := source.observeCalls; got != disposition.wantCommitted {
					t.Fatalf("commit observer calls = %d, want %d", got, disposition.wantCommitted)
				}
				if got := source.refusedCalls; got != disposition.wantRefused {
					t.Fatalf("refusal observer calls = %d, want %d", got, disposition.wantRefused)
				}
				if disposition.wantRefused == 1 {
					if source.refused != testCommitRefusal {
						t.Fatalf("refusal = %+v, want %+v", source.refused, testCommitRefusal)
					}
					if source.refusedItem.WorkItemID != run.store.item.WorkItemID {
						t.Fatalf("refused item = %q, want %q", source.refusedItem.WorkItemID, run.store.item.WorkItemID)
					}
				}
				// A refusal must not record the series at all: the discarded facts
				// never landed.
				got := int64(0)
				if metricRecorded(run.rm, "eshu_dp_workflow_claim_facts_emitted_total") {
					got = collectorCounterValue(t, run.rm, "eshu_dp_workflow_claim_facts_emitted_total", map[string]string{
						"collector_kind": string(scope.CollectorGit),
						"source_system":  "git",
					})
				}
				if got != disposition.wantFacts {
					t.Fatalf("eshu_dp_workflow_claim_facts_emitted_total = %d, want %d", got, disposition.wantFacts)
				}
				outcome := claimedHistogramOutcome(t, run.rm, "eshu_dp_workflow_claim_run_duration_seconds")
				if outcome != disposition.wantOutcome {
					t.Fatalf("run outcome = %q, want %q", outcome, disposition.wantOutcome)
				}
			})
		}
	}
}

// TestClaimedServiceHoldsACommitOnlyObserverOnARefusal is the source-neutral
// hold: a source that implements only the commit observer (the shape of a
// progress marker) hears nothing on a refusal, so its marker cannot advance.
func TestClaimedServiceHoldsACommitOnlyObserverOnARefusal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
	source := &stubClaimedSourceWithObserver{
		stubClaimedSource: &stubClaimedSource{collected: successCollectedGeneration(t, now), ok: true},
	}
	run := runRefusalClaim(t, source, newMarkingCommitter(func(ctx context.Context) {
		MarkCommitRefused(ctx, testCommitRefusal)
	}, nil))

	if run.err != nil {
		t.Fatalf("Run() error = %v, want nil", run.err)
	}
	if got := source.observeCalls; got != 0 {
		t.Fatalf("commit observer calls = %d, want 0 on a refusal", got)
	}
	if got := run.store.completeCalls; got != 1 {
		t.Fatalf("complete calls = %d, want 1", got)
	}
}

// TestClaimedServiceRefusalObserverErrorIsNonFatalAndCounted mirrors the commit
// observer's failure contract: the claim completes, and the error shows on a
// span event and the existing observer failure counter.
func TestClaimedServiceRefusalObserverErrorIsNonFatalAndCounted(t *testing.T) {
	t.Parallel()

	source := refusalTestSource(t, false, false)
	source.refusedErr = errors.New("source state store unavailable")
	run := runRefusalClaim(t, source, newMarkingCommitter(func(ctx context.Context) {
		MarkCommitRefused(ctx, testCommitRefusal)
	}, nil))

	if run.err != nil {
		t.Fatalf("Run() error = %v, want nil: a refusal observer error is non-fatal", run.err)
	}
	if got := run.store.completeCalls; got != 1 {
		t.Fatalf("complete calls = %d, want 1", got)
	}
	if !hasSpanEvent(run.spans.Ended(), "claimed_generation_refusal_observer_failed") {
		t.Fatal("no claimed_generation_refusal_observer_failed span event")
	}
	got := collectorCounterValue(t, run.rm, "eshu_dp_workflow_claim_commit_observer_failures_total", map[string]string{
		"collector_kind": string(scope.CollectorGit),
		"source_system":  "git",
	})
	if got != 1 {
		t.Fatalf("eshu_dp_workflow_claim_commit_observer_failures_total = %d, want 1", got)
	}
}

// TestClaimedServiceIgnoresTheBoxWhenTheCommitFails proves the disposition is
// read only after a nil commit: an error takes the retry path whatever the box
// holds, and neither observer fires.
func TestClaimedServiceIgnoresTheBoxWhenTheCommitFails(t *testing.T) {
	t.Parallel()

	source := refusalTestSource(t, false, false)
	run := runRefusalClaim(t, source, newMarkingCommitter(func(ctx context.Context) {
		MarkCommitRefused(ctx, testCommitRefusal)
	}, errors.New("commit lost")))

	if got := run.store.retryableFailCalls; got != 1 {
		t.Fatalf("retryable fail calls = %d, want 1", got)
	}
	if source.observeCalls != 0 || source.refusedCalls != 0 {
		t.Fatalf("observer calls = %d committed, %d refused, want none", source.observeCalls, source.refusedCalls)
	}
}
