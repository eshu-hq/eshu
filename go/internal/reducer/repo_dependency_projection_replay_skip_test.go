// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// outcomeWorkloadReplayer is a replayer that also reports the closed replay
// outcome, keyed by generation id.
type outcomeWorkloadReplayer struct {
	mu       sync.Mutex
	calls    []workloadMaterializationReplayCall
	outcomes map[string]WorkloadMaterializationReplayOutcome
}

func (r *outcomeWorkloadReplayer) ReplayWorkloadMaterialization(
	ctx context.Context,
	scopeID, generationID, entityKey string,
) (bool, error) {
	outcome, err := r.ReplayWorkloadMaterializationOutcome(ctx, scopeID, generationID, entityKey)
	return outcome == WorkloadMaterializationReplayScheduled, err
}

func (r *outcomeWorkloadReplayer) ReplayWorkloadMaterializationOutcome(
	_ context.Context,
	scopeID, generationID, entityKey string,
) (WorkloadMaterializationReplayOutcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, workloadMaterializationReplayCall{
		scopeID: scopeID, generationID: generationID, entityKey: entityKey,
	})
	if outcome, ok := r.outcomes[generationID]; ok {
		return outcome, nil
	}
	return WorkloadMaterializationReplayScheduled, nil
}

// freshnessByGeneration answers the generation freshness check per generation
// id and counts every lookup.
type freshnessByGeneration struct {
	mu      sync.Mutex
	lookups map[string]int
	results map[string]error
	current map[string]bool
	// sequence, when set for a generation, answers each successive lookup
	// from its list and repeats the last answer.
	sequence map[string][]bool
}

func (f *freshnessByGeneration) check(_ context.Context, _, generationID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lookups == nil {
		f.lookups = map[string]int{}
	}
	f.lookups[generationID]++
	if err := f.results[generationID]; err != nil {
		return false, err
	}
	if answers := f.sequence[generationID]; len(answers) > 0 {
		index := f.lookups[generationID] - 1
		if index >= len(answers) {
			index = len(answers) - 1
		}
		return answers[index], nil
	}
	return f.current[generationID], nil
}

func repoDependencyReplaySkipFixture(
	generations ...string,
) (*fakeRepoDependencyIntentStore, *recordingCodeCallProjectionEdgeWriter, time.Time) {
	now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_fixture01"
	rows := make([]SharedProjectionIntentRow, 0, len(generations))
	for i, generationID := range generations {
		rows = append(rows, repoDependencyIntentRow(
			"intent-"+generationID, "scope-"+generationID, repoID, repoID, "run-"+generationID, generationID,
			now.Add(time.Duration(i)*time.Second),
			map[string]any{
				"repo_id":           repoID,
				"target_repo_id":    "repository:r_target_" + generationID,
				"relationship_type": "DEPENDS_ON",
				"evidence_source":   defaultEvidenceSource,
			},
		))
	}
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         rows,
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: rows},
		leaseGranted:            true,
	}
	return reader, &recordingCodeCallProjectionEdgeWriter{}, now
}

// acceptByRunSuffix accepts run-<gen> as generation <gen>, which is how a
// code-import source run keeps pointing at a generation recover-generations
// retired: the acceptance row is not rewritten.
func acceptByRunSuffix(key SharedProjectionAcceptanceKey) (string, bool) {
	return strings.TrimPrefix(key.SourceRunID, "run-"), true
}

func newReplaySkipInstruments(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

func replaySkipCounter(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	return repoDependencyReasonCounter(t, reader, "eshu_dp_repo_dependency_replay_skipped_total", reason)
}

func generationAnomalyCounter(t *testing.T, reader *sdkmetric.ManualReader, reason string) int64 {
	t.Helper()
	return repoDependencyReasonCounter(t, reader, "eshu_dp_repo_dependency_generation_anomalies_total", reason)
}

func repoDependencyReasonCounter(
	t *testing.T,
	reader *sdkmetric.ManualReader,
	metricName, reason string,
) int64 {
	t.Helper()
	var resources metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resources); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range resources.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != metricName {
				continue
			}
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				if got, ok := point.Attributes.Value(attribute.Key("reason")); ok && got.AsString() == reason {
					return point.Value
				}
			}
		}
	}
	return 0
}

// TestRepoDependencyReplaySkipsSupersededGenerationRequest is the #7670
// regression: recover-generations retired the scope generation while the
// acceptance row still named it, so the replay for it was reported unscheduled
// and quarantined the lane every cycle.
func TestRepoDependencyReplaySkipsSupersededGenerationRequest(t *testing.T) {
	t.Parallel()

	reader, writer, now := repoDependencyReplaySkipFixture("gen-superseded")
	replayer := &recordingWorkloadMaterializationReplayer{reject: true}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-superseded": false}}
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
		t.Fatalf("processOnce() error = %v, want nil: a superseded generation has nothing to replay", err)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d: the intent must drain", got, want)
	}
	if got := len(replayer.calls); got != 0 {
		t.Fatalf("replayer calls = %d, want 0 for a superseded generation", got)
	}
	if got := len(reader.marked); got != 1 {
		t.Fatalf("marked completed = %d, want 1", got)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 1 {
		t.Fatalf("inactive_generation skip counter = %d, want 1", got)
	}
}

// TestRepoDependencyReplayFailsClosedOnSupersededItemOnActiveGeneration pins
// the arbiter's ruling: a superseded stable item is nothing to replay only for
// a retired generation. On the scope's active generation the queue never
// revives the item, so the owed materialization cannot run and the cycle must
// fail closed, quarantine the lease, keep the intents pending, and count the
// state under its own reason.
func TestRepoDependencyReplayFailsClosedOnSupersededItemOnActiveGeneration(t *testing.T) {
	t.Parallel()

	reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
	replayer := &outcomeWorkloadReplayer{outcomes: map[string]WorkloadMaterializationReplayOutcome{
		"gen-a": WorkloadMaterializationReplaySuperseded,
	}}
	freshness := &freshnessByGeneration{current: map[string]bool{"gen-a": true}}
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

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "workload materialization replay was not scheduled") {
		t.Fatalf("processOnce() error = %v, want the unscheduled-replay error", err)
	}
	if !strings.Contains(err.Error(), "quarantine repo dependency lease") {
		t.Fatalf("processOnce() error = %v, want the lease quarantine", err)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked completed = %d, want 0: the fence must hold the intents", got)
	}
	if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 1 {
		t.Fatalf("superseded_item_on_active_generation counter = %d, want 1", got)
	}
	if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 0 {
		t.Fatalf("inactive_generation skip counter = %d, want 0 on a blocked request", got)
	}
}

// TestRepoDependencyReplayStillFailsClosedOnActiveGeneration pins the fence: a
// request on the scope's active generation that cannot be scheduled keeps
// failing the cycle, with or without the outcome-aware replayer.
func TestRepoDependencyReplayStillFailsClosedOnActiveGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		replayer WorkloadMaterializationReplayer
	}{
		{name: "boolean replayer", replayer: &recordingWorkloadMaterializationReplayer{reject: true}},
		{name: "outcome replayer", replayer: &outcomeWorkloadReplayer{
			outcomes: map[string]WorkloadMaterializationReplayOutcome{
				"gen-a": WorkloadMaterializationReplayNotScheduled,
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
			freshness := &freshnessByGeneration{current: map[string]bool{"gen-a": true}}
			instruments, metricReader := newReplaySkipInstruments(t)
			runner := RepoDependencyProjectionRunner{
				IntentReader:                    reader,
				LeaseManager:                    reader,
				AcceptanceUnitGate:              reader,
				EdgeWriter:                      writer,
				WorkloadMaterializationReplayer: tt.replayer,
				GenerationFreshness:             freshness.check,
				AcceptedGen:                     acceptByRunSuffix,
				Instruments:                     instruments,
				Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
			}

			_, err := runner.processOnce(context.Background(), now)
			if err == nil || !strings.Contains(err.Error(), "workload materialization replay was not scheduled") {
				t.Fatalf("processOnce() error = %v, want the unscheduled-replay error", err)
			}
			if got := len(reader.marked); got != 0 {
				t.Fatalf("marked completed = %d, want 0: the fence must hold the intents", got)
			}
			if got := replaySkipCounter(t, metricReader, telemetry.RepoDependencyReplaySkipInactiveGeneration); got != 0 {
				t.Fatalf("skip counter = %d, want 0 on a fail-closed request", got)
			}
			if got := generationAnomalyCounter(t, metricReader, telemetry.RepoDependencyAnomalySupersededItemOnActiveGeneration); got != 0 {
				t.Fatalf("anomaly counter = %d, want 0 for an item that is not superseded", got)
			}
		})
	}
}

// TestRepoDependencyReplayFreshnessEdgeCases covers the lookup outcomes other
// than a plain current or superseded generation.
func TestRepoDependencyReplayFreshnessEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("lookup error fails closed", func(t *testing.T) {
		t.Parallel()
		reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
		replayer := &recordingWorkloadMaterializationReplayer{}
		freshness := &freshnessByGeneration{results: map[string]error{"gen-a": errors.New("postgres unavailable")}}
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
		_, err := runner.processOnce(context.Background(), now)
		if err == nil || !strings.Contains(err.Error(), "postgres unavailable") {
			t.Fatalf("processOnce() error = %v, want the lookup error", err)
		}
		if got := len(reader.marked); got != 0 {
			t.Fatalf("marked completed = %d, want 0", got)
		}
	})

	t.Run("pending newer generation still replays", func(t *testing.T) {
		t.Parallel()
		reader, writer, now := repoDependencyReplaySkipFixture("gen-a")
		replayer := &recordingWorkloadMaterializationReplayer{}
		freshness := &freshnessByGeneration{results: map[string]error{
			"gen-a": reducercontract.GenerationNotYetActiveError{
				ScopeID: "scope-gen-a", GenerationID: "gen-a", ActiveGenerationID: "gen-old",
			},
		}}
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
			t.Fatalf("processOnce() error = %v, want nil", err)
		}
		if got := len(replayer.calls); got != 1 {
			t.Fatalf("replayer calls = %d, want 1: a pending generation is not retired", got)
		}
	})

	t.Run("mixed superseded and active requests", func(t *testing.T) {
		t.Parallel()
		reader, writer, now := repoDependencyReplaySkipFixture("gen-active", "gen-retired")
		replayer := &recordingWorkloadMaterializationReplayer{}
		freshness := &freshnessByGeneration{current: map[string]bool{"gen-active": true, "gen-retired": false}}
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
		result, err := runner.processOnce(context.Background(), now)
		if err != nil {
			t.Fatalf("processOnce() error = %v, want nil", err)
		}
		if got, want := result.ProcessedIntents, 2; got != want {
			t.Fatalf("ProcessedIntents = %d, want %d", got, want)
		}
		if got := len(replayer.calls); got != 1 || replayer.calls[0].generationID != "gen-active" {
			t.Fatalf("replayer calls = %+v, want exactly the active generation", replayer.calls)
		}
	})

	t.Run("one lookup per scope generation", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
		repoID := "repository:r_fixture01"
		mkRow := func(id, target string) SharedProjectionIntentRow {
			return repoDependencyIntentRow(id, "scope-a", repoID, repoID, "run-gen-a", "gen-a", now, map[string]any{
				"repo_id": repoID, "target_repo_id": target,
				"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "evidence_source": defaultEvidenceSource,
			})
		}
		rows := []SharedProjectionIntentRow{
			mkRow("i1", "repository:r_t1"), mkRow("i2", "repository:r_t2"), mkRow("i3", "repository:r_t1"),
		}
		reader := &fakeRepoDependencyIntentStore{
			pendingByDomain:         rows,
			pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: rows},
			leaseGranted:            true,
		}
		replayer := &recordingWorkloadMaterializationReplayer{}
		freshness := &freshnessByGeneration{current: map[string]bool{"gen-a": true}}
		runner := RepoDependencyProjectionRunner{
			IntentReader:                    reader,
			LeaseManager:                    reader,
			AcceptanceUnitGate:              reader,
			EdgeWriter:                      &recordingCodeCallProjectionEdgeWriter{},
			WorkloadMaterializationReplayer: replayer,
			GenerationFreshness:             freshness.check,
			AcceptedGen:                     acceptByRunSuffix,
			Config:                          RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
		}
		if _, err := runner.processOnce(context.Background(), now); err != nil {
			t.Fatalf("processOnce() error = %v", err)
		}
		if got, want := len(replayer.calls), 2; got != want {
			t.Fatalf("replayer calls = %d, want %d distinct entities", got, want)
		}
		if got := freshness.lookups["gen-a"]; got != 1 {
			t.Fatalf("freshness lookups for gen-a = %d, want 1", got)
		}
	})
}
