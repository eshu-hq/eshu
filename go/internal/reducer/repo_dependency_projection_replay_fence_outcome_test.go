// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRepoDependencyRunsOnFencedReplayCountsSupersededItemOnActiveGeneration
// pins the fenced outcome split (#7670 remainder): a fenced replay that
// reports superseded on the scope's active generation counts as
// superseded_item_on_active_generation — the same anomaly class as the plain
// path — not as unscheduled_fenced_replay_on_active_generation, and still
// fails the cycle closed.
func TestRepoDependencyRunsOnFencedReplayCountsSupersededItemOnActiveGeneration(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-active")
	replayer := &outcomeFenceWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-active": WorkloadMaterializationReplaySuperseded,
	}}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-active": true}}
	instruments, metricReader := newReplaySkipInstruments(t)
	runner := RepoDependencyProjectionRunner{
		Instruments:                     instruments,
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			_ context.Context, keys []GraphProjectionPhaseKey, _ GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			return func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) { return false, false }, nil
		},
		GenerationFreshness: freshness.check,
		AcceptedGen:         acceptByRunSuffix,
		Config:              RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "fenced replay was not scheduled") {
		t.Fatalf("processOnce() error = %v, want the fenced unscheduled-replay error", err)
	}
	if !strings.Contains(err.Error(), "quarantine repo dependency lease") {
		t.Fatalf("processOnce() error = %v, want the lease quarantine", err)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 1 {
		t.Fatalf("superseded_item_on_active_generation counter = %d, want 1", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration); got != 0 {
		t.Fatalf("unscheduled_fenced_replay_on_active_generation counter = %d, want 0 for a superseded item", got)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 0 {
		t.Fatalf("inactive_generation skip counter = %d, want 0 on a blocked request", got)
	}
}

// TestRepoDependencyRunsOnFencedReplayCountsDeadLetteredItemAsUnscheduled pins
// the other half of the split: a fenced replay that reports anything but
// scheduled or superseded (a dead-lettered stable item here) keeps counting
// as unscheduled_fenced_replay_on_active_generation and fails closed.
func TestRepoDependencyRunsOnFencedReplayCountsDeadLetteredItemAsUnscheduled(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-active")
	replayer := &outcomeFenceWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-active": WorkloadMaterializationReplayNotScheduled,
	}}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-active": true}}
	instruments, metricReader := newReplaySkipInstruments(t)
	runner := RepoDependencyProjectionRunner{
		Instruments:                     instruments,
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			_ context.Context, keys []GraphProjectionPhaseKey, _ GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			return func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) { return false, false }, nil
		},
		GenerationFreshness: freshness.check,
		AcceptedGen:         acceptByRunSuffix,
		Config:              RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "fenced replay was not scheduled") {
		t.Fatalf("processOnce() error = %v, want the fenced unscheduled-replay error", err)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration); got != 1 {
		t.Fatalf("unscheduled_fenced_replay_on_active_generation counter = %d, want 1", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 0 {
		t.Fatalf("superseded_item_on_active_generation counter = %d, want 0 for a dead-lettered item", got)
	}
}

// TestRepoDependencyRunsOnFencedReplayOutcomeSkipsWhenSupersedeLandsBetweenCheckAndReplay
// runs the supersede race through the outcome path: the first check reads the
// generation as current, the fenced replay reports superseded, and the uncached
// re-check reads the generation as retired. The request is skipped with no
// anomaly count, exactly like the boolean path's race.
func TestRepoDependencyRunsOnFencedReplayOutcomeSkipsWhenSupersedeLandsBetweenCheckAndReplay(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-a")
	replayer := &outcomeFenceWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-a": WorkloadMaterializationReplaySuperseded,
	}}
	freshness := &freshnessByGeneration{sequence: map[string][]bool{"gen-a": {true, false}}}
	instruments, metricReader := newReplaySkipInstruments(t)
	runner := RepoDependencyProjectionRunner{
		Instruments:                     instruments,
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			context.Context, []GraphProjectionPhaseKey, GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			return func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) { return false, false }, nil
		},
		GenerationFreshness: freshness.check,
		AcceptedGen:         acceptByRunSuffix,
		Config:              RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	if _, err := runner.processOnce(context.Background(), now); err != nil {
		t.Fatalf("processOnce() error = %v, want nil once the generation reads as retired", err)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 1 {
		t.Fatalf("inactive_generation skip counter = %d, want 1", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 0 {
		t.Fatalf("superseded_item_on_active_generation counter = %d, want 0 on a skipped race", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration); got != 0 {
		t.Fatalf("unscheduled_fenced_replay_on_active_generation counter = %d, want 0 on a skipped race", got)
	}
}
