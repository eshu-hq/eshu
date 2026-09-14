// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// coveringBackfillOrderProbe observes each backfill relative to projector
// quiescence. The pipeline calls it sequentially, so the fields need no lock.
type coveringBackfillOrderProbe struct {
	*fakeCommitter
	projectorQuiesced       chan struct{}
	enters                  int
	secondEnterAfterQuiesce bool
	failSecondErr           error
}

func (p *coveringBackfillOrderProbe) BackfillAllRelationshipEvidence(
	ctx context.Context,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) error {
	p.enters++
	if p.enters == 2 {
		select {
		case <-p.projectorQuiesced:
			p.secondEnterAfterQuiesce = true
		default:
		}
		if p.failSecondErr != nil {
			p.mu.Lock()
			p.calls = append(p.calls, "backfill")
			p.backfillCalls++
			p.mu.Unlock()
			return p.failSecondErr
		}
	}
	return p.fakeCommitter.BackfillAllRelationshipEvidence(ctx, tracer, instruments)
}

// drainOrderSignalingRunner closes done when projection returns, before the
// sink acknowledgement that completes the projector drain.
type drainOrderSignalingRunner struct {
	delay time.Duration
	done  chan struct{}
	once  sync.Once
}

func (r *drainOrderSignalingRunner) Project(
	ctx context.Context,
	_ scope.IngestionScope,
	_ scope.ScopeGeneration,
	_ []facts.Envelope,
) (projector.Result, error) {
	select {
	case <-ctx.Done():
		return projector.Result{}, ctx.Err()
	case <-time.After(r.delay):
	}
	r.once.Do(func() { close(r.done) })
	return projector.Result{}, nil
}

func TestPipelinedBootstrapRunsCoveringBackfillAfterProjectorDrain(t *testing.T) {
	t.Parallel()

	quiesced := make(chan struct{})
	sink := &concurrentWorkSink{}
	probe := &coveringBackfillOrderProbe{
		fakeCommitter:     &fakeCommitter{},
		projectorQuiesced: quiesced,
	}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: probe},
		projectorDeps{
			workSource: &concurrentWorkSource{
				items: []projector.ScopeGenerationWork{{Scope: scope.IngestionScope{ScopeID: "s1"}}},
			},
			factStore: &fakeFactStore{},
			runner:    &drainOrderSignalingRunner{delay: 50 * time.Millisecond, done: quiesced},
			workSink:  sink,
		},
		2,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runPipelined() error = %v, want nil", err)
	}
	if got := sink.acked.Load(); got != 1 {
		t.Fatalf("projector not drained: acked=%d, want 1", got)
	}
	if got := probe.enters; got != 2 {
		t.Fatalf("backfill calls = %d, want 2 (initial + post-drain covering)", got)
	}
	if !probe.secondEnterAfterQuiesce {
		t.Fatal("covering backfill entered before projector quiesced")
	}
	if got, want := probe.snapshotCalls(), []string{"backfill", "backfill", "iac_reachability", "reopen", "reopen_code_import", "reopen_correlation", "enqueue_drift"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}

func TestPipelinedBootstrapCoveringBackfillFailureIsFatal(t *testing.T) {
	t.Parallel()

	coveringErr := errors.New("covering backfill failed")
	probe := &coveringBackfillOrderProbe{
		fakeCommitter:     &fakeCommitter{},
		projectorQuiesced: make(chan struct{}),
		failSecondErr:     coveringErr,
	}

	err := runPipelined(
		context.Background(),
		collectorDeps{source: &fakeSource{generations: nil}, committer: probe},
		projectorDeps{
			workSource: &concurrentWorkSource{items: nil},
			factStore:  &fakeFactStore{},
			runner:     &fakeProjectionRunner{},
			workSink:   &concurrentWorkSink{},
		},
		2,
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("runPipelined() error = nil, want non-nil")
	}
	if !errors.Is(err, coveringErr) {
		t.Fatalf("runPipelined() error = %v, want wrapping %v", err, coveringErr)
	}
	if got, want := probe.snapshotCalls(), []string{"backfill", "backfill"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("workflow calls = %v, want %v", got, want)
	}
}
