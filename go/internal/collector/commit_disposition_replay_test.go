// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// replayDispositions are the rows both services share: a refused generation was
// rolled back, so completing dead-letter replay against it would mark rows
// replayed by a generation that does not exist (#10037). An unchanged skip means
// the scope already holds the content, so it stays a completed replay.
//
// wantCommittedFacts and wantDrainCalls cover the unclaimed Service (#10068). A
// refusal stored nothing, so it counts no committed facts and sets no drain
// latch: the startup once-latch opens one epoch, and the empty-batch escape then
// keeps the shard arriving join-only. An unchanged skip keeps its behavior.
var replayDispositions = []struct {
	name               string
	mark               func(context.Context)
	wantCompletions    int
	wantCommittedFacts bool
	wantDrainCalls     []bool
	// wantDrainCallsAfterStartup holds when the generation lands after the
	// startup once-latch is spent, so only committedSinceDrain can report true.
	wantDrainCallsAfterStartup []bool
}{
	{
		name:                       "committed",
		mark:                       func(context.Context) {},
		wantCompletions:            1,
		wantCommittedFacts:         true,
		wantDrainCalls:             []bool{true},
		wantDrainCallsAfterStartup: []bool{true, true},
	},
	{
		name:                       "unchanged skip",
		mark:                       MarkCommitUnchangedSkip,
		wantCompletions:            1,
		wantCommittedFacts:         true,
		wantDrainCalls:             []bool{true},
		wantDrainCallsAfterStartup: []bool{true, true},
	},
	{
		name:                       "refused",
		mark:                       func(ctx context.Context) { MarkCommitRefused(ctx, testCommitRefusal) },
		wantCompletions:            0,
		wantDrainCalls:             []bool{true, false},
		wantDrainCallsAfterStartup: []bool{true, false},
	},
}

func TestClaimedServiceCompletesDeadLetterReplayOnlyOnALandedCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			now := time.Date(2026, time.November, 4, 12, 0, 0, 0, time.UTC)
			item := testClaimedWorkItem(now)
			item.SourceSystem = "git"
			claim := testWorkflowClaim(item.WorkItemID, now)
			store := &stubClaimStore{item: item, claim: claim, found: true}
			store.heartbeat = func(context.Context, workflow.ClaimMutation) error {
				cancel()
				return nil
			}
			source := refusalTestSource(t, false, false)
			service := testClaimedService(now, claim, scope.CollectorGit, store, source, newMarkingCommitter(tc.mark, nil))
			sink := &stubGenerationDeadLetterSink{}
			service.DeadLetters = sink

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := store.completeCalls; got != 1 {
				t.Fatalf("complete calls = %d, want 1: a refusal still completes the claim", got)
			}
			if got := len(sink.completions); got != tc.wantCompletions {
				t.Fatalf("replay completions = %d, want %d", got, tc.wantCompletions)
			}
		})
	}
}

func TestServiceCompletesDeadLetterReplayOnlyOnALandedCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			sink := &stubGenerationDeadLetterSink{}
			service := Service{
				Source: &stubSource{collected: []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)}},
				Committer: &stubCommitter{commit: func(
					commitCtx context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
				) error {
					for range stream {
					}
					tc.mark(commitCtx)
					cancel()
					return nil
				}},
				DeadLetters:  sink,
				PollInterval: time.Millisecond,
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if got := len(sink.completions); got != tc.wantCompletions {
				t.Fatalf("replay completions = %d, want %d", got, tc.wantCompletions)
			}
		})
	}
}

func TestServiceRefusedCommitAddsNoCommittedFacts(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			metricReader := sdkmetric.NewManualReader()
			meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
			instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			service := Service{
				Source:       &stubSource{collected: []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)}},
				Committer:    markingServiceCommitter(tc.mark, cancel),
				Instruments:  instruments,
				PollInterval: time.Millisecond,
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}

			var rm metricdata.ResourceMetrics
			if err := metricReader.Collect(context.Background(), &rm); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			wantCommitted := int64(0)
			if tc.wantCommittedFacts {
				wantCommitted = int64(len(envelopes))
			}
			if got := collectorCounterTotal(rm, "eshu_dp_facts_committed_total"); got != wantCommitted {
				t.Fatalf("eshu_dp_facts_committed_total = %d, want %d", got, wantCommitted)
			}
			if got, want := collectorCounterTotal(rm, "eshu_dp_facts_emitted_total"), int64(len(envelopes)); got != want {
				t.Fatalf("eshu_dp_facts_emitted_total = %d, want %d: the facts were produced", got, want)
			}
		})
	}
}

func TestServiceFailedCommitAddsNoCommittedFacts(t *testing.T) {
	t.Parallel()

	scopeValue, generationValue, envelopes := testCollectedGeneration()
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("collector-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	service := Service{
		Committer: &stubCommitter{commit: func(
			_ context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
		) error {
			for range stream {
			}
			return errors.New("commit failed")
		}},
		Instruments:  instruments,
		PollInterval: time.Millisecond,
	}

	commitCtx, _ := WithCommitDisposition(context.Background())
	collected := FactsFromSlice(scopeValue, generationValue, envelopes)
	if err := service.commitWithTelemetry(commitCtx, collected, time.Now()); err == nil {
		t.Fatal("commitWithTelemetry() error = nil, want the commit failure")
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := collectorCounterTotal(rm, "eshu_dp_facts_committed_total"); got != 0 {
		t.Fatalf("eshu_dp_facts_committed_total = %d, want 0", got)
	}
}

func TestServiceRefusedCommitIsNotADrainCommit(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			emptyPolls := 0
			var drainCalls []bool
			service := Service{
				Source: &stubSource{
					collected: []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)},
					empty: func() {
						emptyPolls++
						if emptyPolls == 2 {
							cancel()
						}
					},
				},
				Committer:              markingServiceCommitter(tc.mark, func() {}),
				PollInterval:           time.Millisecond,
				AfterEmptyBatchDrained: true,
				AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
					drainCalls = append(drainCalls, hasCommitted)
					return nil
				},
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if !slices.Equal(drainCalls, tc.wantDrainCalls) {
				t.Fatalf("AfterBatchDrained hasCommitted calls = %v, want %v", drainCalls, tc.wantDrainCalls)
			}
		})
	}
}

// TestServiceRefusedCommitOpensNoEpochAfterStartup spends the startup once-latch
// on an idle poll first, then lands the generation. A refusal must then report
// hasCommitted=false, or the barrier reruns corpus-wide maintenance against an
// unchanged corpus (#10068).
func TestServiceRefusedCommitOpensNoEpochAfterStartup(t *testing.T) {
	t.Parallel()

	for _, tc := range replayDispositions {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scopeValue, generationValue, envelopes := testCollectedGeneration()
			source := &stubSource{}
			emptyPolls := 0
			source.empty = func() {
				emptyPolls++
				switch emptyPolls {
				case 1:
					source.collected = []CollectedGeneration{FactsFromSlice(scopeValue, generationValue, envelopes)}
				case 2:
					cancel()
				}
			}
			var drainCalls []bool
			service := Service{
				Source:                 source,
				Committer:              markingServiceCommitter(tc.mark, func() {}),
				PollInterval:           time.Millisecond,
				AfterEmptyBatchDrained: true,
				AfterBatchDrained: func(_ context.Context, hasCommitted bool) error {
					drainCalls = append(drainCalls, hasCommitted)
					return nil
				},
			}

			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			if !slices.Equal(drainCalls, tc.wantDrainCallsAfterStartup) {
				t.Fatalf("AfterBatchDrained hasCommitted calls = %v, want %v", drainCalls, tc.wantDrainCallsAfterStartup)
			}
		})
	}
}

// markingServiceCommitter drains the stream, marks the commit's box, runs after,
// and returns nil, as a committer does when it rolls a generation back on purpose.
func markingServiceCommitter(mark func(context.Context), after func()) *stubCommitter {
	return &stubCommitter{commit: func(
		ctx context.Context, _ scope.IngestionScope, _ scope.ScopeGeneration, stream <-chan facts.Envelope,
	) error {
		for range stream {
		}
		mark(ctx)
		after()
		return nil
	}}
}

// collectorCounterTotal sums one counter across its attribute sets. A counter
// that was never added to is absent from the collection and reads as zero.
func collectorCounterTotal(rm metricdata.ResourceMetrics, metricName string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			if sum, ok := metricRecord.Data.(metricdata.Sum[int64]); ok {
				for _, dp := range sum.DataPoints {
					total += dp.Value
				}
			}
		}
	}
	return total
}
