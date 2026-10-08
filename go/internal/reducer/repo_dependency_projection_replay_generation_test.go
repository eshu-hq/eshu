// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRepoDependencyReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay covers
// the concurrent-supersede window: the freshness check reads the generation as
// current, a supersede lands, and the replay then finds the stable item
// superseded. The re-check reads the generation as retired, so the request is
// skipped rather than reported as an active-generation failure.
func TestRepoDependencyReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay(t *testing.T) {
	t.Parallel()

	reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
	replayer := &outcomeWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-a": WorkloadMaterializationReplaySuperseded,
	}}
	freshness := &freshnessByGeneration{sequence: map[string][]bool{"gen-a": {true, false}}}
	instruments, metricReader := newReplaySkipInstruments(t)
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		GenerationFreshness:             freshness.check,
		AcceptedGen:                     acceptByRunSuffix,
		Instruments:                     instruments,
		Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil once the generation reads as retired", err)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 1 {
		t.Fatalf("inactive_generation skip counter = %d, want 1", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 0 {
		t.Fatalf("superseded_item_on_active_generation counter = %d, want 0", got)
	}
}

// TestRepoDependencyReplayFailsClosedOnSupersededItemWithoutFreshness: with no
// freshness seam the runner cannot prove the generation is retired, so a
// superseded stable item fails the cycle closed.
func TestRepoDependencyReplayFailsClosedOnSupersededItemWithoutFreshness(t *testing.T) {
	t.Parallel()

	reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
	replayer := &outcomeWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-a": WorkloadMaterializationReplaySuperseded,
	}}
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		AcceptedGen:                     acceptByRunSuffix,
		Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "workload materialization replay was not scheduled") {
		t.Fatalf("processOnce() error = %v, want the unscheduled-replay error", err)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %d, want 0", got)
	}
}

// TestRepoDependencyReplayRechecksFreshnessEachCycle proves the freshness cache
// lives for one cycle only: a scope can activate a new generation between
// cycles, so the second cycle must ask again and then skip the replay.
func TestRepoDependencyReplayRechecksFreshnessEachCycle(t *testing.T) {
	t.Parallel()

	reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
	replayer := &recordingWorkloadMaterializationReplayer{}
	freshness := &freshnessByGeneration{sequence: map[string][]bool{"gen-a": {true, false}}}
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		GenerationFreshness:             freshness.check,
		AcceptedGen:                     acceptByRunSuffix,
		Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	if _, err := runner.processOnce(context.Background(), now); err != nil {
		t.Fatalf("first processOnce() error = %v", err)
	}
	if got := len(replayer.calls); got != 1 {
		t.Fatalf("replayer calls after the active cycle = %d, want 1", got)
	}
	// The first cycle completed its intents; make them pending again, as new
	// intents on the same accepted generation would be.
	for i := range reader.pendingByDomain {
		reader.pendingByDomain[i].CompletedAt = nil
	}
	for key := range reader.pendingByAcceptanceUnit {
		for i := range reader.pendingByAcceptanceUnit[key] {
			reader.pendingByAcceptanceUnit[key][i].CompletedAt = nil
		}
	}
	if _, err := runner.processOnce(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatalf("second processOnce() error = %v", err)
	}
	if got := freshness.lookups["gen-a"]; got != 2 {
		t.Fatalf("freshness lookups across two cycles = %d, want 2: the cache must not outlive a cycle", got)
	}
	if got := len(replayer.calls); got != 1 {
		t.Fatalf("replayer calls after the retired cycle = %d, want still 1", got)
	}
}

// TestRepoDependencyInactiveAcceptedGenerationRowsAreCountedAndLogged covers
// the visibility half: rows whose accepted generation is retired still
// project, are counted by row, and log one WARN per scope generation.
func TestRepoDependencyInactiveAcceptedGenerationRowsAreCountedAndLogged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_fixture01"
	mk := func(id, generationID, target string) SharedProjectionIntentRow {
		return repoDependencyIntentRow(
			id, "scope-"+generationID, repoID, repoID, "run-"+generationID, generationID, now,
			map[string]any{
				"repo_id": repoID, "target_repo_id": target,
				"relationship_type": "DEPENDS_ON", "evidence_source": defaultEvidenceSource,
			},
		)
	}
	rows := []SharedProjectionIntentRow{
		mk("r1", "gen-retired", "repository:r_t1"),
		mk("r2", "gen-retired", "repository:r_t2"),
		mk("a1", "gen-active", "repository:r_t3"),
	}
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         rows,
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: rows},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-retired": false, "gen-active": true}}
	instruments, metricReader := newReplaySkipInstruments(t)
	var logs bytes.Buffer
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: &recordingWorkloadMaterializationReplayer{},
		GenerationFreshness:             freshness.check,
		AcceptedGen:                     acceptByRunSuffix,
		Instruments:                     instruments,
		Logger:                          slog.New(slog.NewJSONHandler(&logs, nil)),
		Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if got, want := result.ProcessedIntents, 3; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d: the rows must still drain", got, want)
	}
	if got := len(writer.writeCalls); got == 0 {
		t.Fatal("write calls = 0, want the rows to still project")
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyInactiveAcceptedGeneration); got != 2 {
		t.Fatalf("inactive_accepted_generation counter = %d, want 2 rows on the retired generation", got)
	}
	warns := 0
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if strings.Contains(line, `"msg":"repo dependency generation anomaly"`) {
			warns++
			if !strings.Contains(line, `"scope_id":"scope-gen-retired"`) ||
				!strings.Contains(line, `"generation_id":"gen-retired"`) ||
				!strings.Contains(line, `"reason":"inactive_accepted_generation"`) {
				t.Fatalf("anomaly log line = %s, want the retired scope, generation and reason", line)
			}
		}
	}
	if warns != 1 {
		t.Fatalf("anomaly WARN lines = %d, want 1 per retired scope generation", warns)
	}
}

func runsOnRetiredGenerationFixture(
	t *testing.T,
	generationID string,
) (*fakeRepoDependencyIntentStore, *recordingCodeCallProjectionEdgeWriter, time.Time) {
	t.Helper()
	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	repoID := "repo-source"
	runsOn := repoDependencyIntentRow(
		"runs-on-1", "scope-"+generationID, repoID, repoID, "run-"+generationID, generationID, now,
		map[string]any{
			"repo_id":           repoID,
			"platform_id":       "platform:kubernetes:none:cluster/prod:none:none",
			"relationship_type": "RUNS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{runsOn},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {runsOn}},
		leaseGranted:            true,
	}
	return reader, &recordingCodeCallProjectionEdgeWriter{}, now
}

// TestRepoDependencyRunsOnFencedReplaySkipsRetiredGeneration settles the fenced
// path. active rows are filtered by the acceptance row, not by the scope's
// active generation, so a RUNS_ON row on a retired accepted generation reaches
// ensureRunsOnWorkloadReadiness. The fenced replay UPDATE excludes superseded
// items, so the store reports it unscheduled and the lane errored the same way
// as the plain replay. No readiness can ever publish for a retired generation,
// so the request is skipped and not waited on.
func TestRepoDependencyRunsOnFencedReplaySkipsRetiredGeneration(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-retired")
	replayer := &recordingWorkloadMaterializationReplayer{reject: true}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-retired": false}}
	instruments, metricReader := newReplaySkipInstruments(t)
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			context.Context, []GraphProjectionPhaseKey, GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			t.Fatal("readiness prefetch ran for a retired generation, want no wait on it")
			return nil, nil
		},
		GenerationFreshness: freshness.check,
		AcceptedGen:         acceptByRunSuffix,
		Instruments:         instruments,
		Config:              RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil for a retired generation's fenced replay", err)
	}
	if got := result.BlockedReadiness; got != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0: a retired generation must not hold the lane", got)
	}
	if got := len(replayer.calls); got != 0 {
		t.Fatalf("replayer calls = %d, want 0 for a retired generation", got)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
	if got := len(reader.marked); got != 1 {
		t.Fatalf("marked completed = %d, want 1: an unmarked intent would livelock the unit", got)
	}
	if got := len(writer.writeCalls); got == 0 {
		t.Fatal("write calls = 0, want the RUNS_ON row to project once nothing is owed")
	}
	// The RUNS_ON row produces a fenced and a plain request for the same scope,
	// generation and entity: one request, one count.
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 1 {
		t.Fatalf("inactive_generation skip counter = %d, want 1 per request, not per path", got)
	}
}

// TestRepoDependencyRunsOnFencedReplayStillFailsClosedOnActiveGeneration pins
// the fence for the fenced path: an unscheduled fenced replay on the scope's
// active generation, which includes a superseded stable item because the
// fenced UPDATE excludes it, keeps failing the cycle.
func TestRepoDependencyRunsOnFencedReplayStillFailsClosedOnActiveGeneration(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-active")
	replayer := &recordingWorkloadMaterializationReplayer{reject: true}
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
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %d, want 0", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration); got != 1 {
		t.Fatalf("unscheduled_fenced_replay_on_active_generation counter = %d, want 1", got)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 0 {
		t.Fatalf("inactive_generation skip counter = %d, want 0 on a blocked request", got)
	}
}

// TestRepoDependencyRunsOnFencedReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay
// mirrors the plain path's re-check: the first check reads the generation as
// current, the supersede lands, the fenced replay is refused, and the uncached
// re-check reads the generation as retired. The request is skipped, not failed;
// the next cycle drops it up front.
func TestRepoDependencyRunsOnFencedReplaySkipsWhenSupersedeLandsBetweenCheckAndReplay(t *testing.T) {
	t.Parallel()

	reader, writer, now := runsOnRetiredGenerationFixture(t, "gen-a")
	replayer := &recordingWorkloadMaterializationReplayer{reject: true}
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
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalyUnscheduledFencedReplayOnActiveGeneration); got != 0 {
		t.Fatalf("unscheduled_fenced_replay_on_active_generation counter = %d, want 0", got)
	}
}
