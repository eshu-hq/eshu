// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type fakeActiveStateSnapshotScopeLister struct {
	scopes []PendingConfigStateDriftScope
	err    error
	calls  []int // limit passed on each call
}

func (f *fakeActiveStateSnapshotScopeLister) ListActiveStateSnapshotScopes(_ context.Context, limit int) ([]PendingConfigStateDriftScope, error) {
	f.calls = append(f.calls, limit)
	if f.err != nil {
		return nil, f.err
	}
	return f.scopes, nil
}

type fakeCatchUpIntentWriter struct {
	got    []ReducerIntent
	result IntentResult
	err    error
}

func (f *fakeCatchUpIntentWriter) Enqueue(_ context.Context, intents []ReducerIntent) (IntentResult, error) {
	f.got = intents
	if f.err != nil {
		return IntentResult{}, f.err
	}
	return f.result, nil
}

// TestConfigStateDriftCatchUpSweeperRunOnceEnqueuesOneIntentPerActiveScope
// proves the issue #5593 P1-1 sweeper builds the same reducer intent shape
// (domain, reason, source_system) the other two config_state_drift producers
// use, for every scope the lister returns, and returns the DB's actual
// insertion count (not len(scopes)) as its own result.
func TestConfigStateDriftCatchUpSweeperRunOnceEnqueuesOneIntentPerActiveScope(t *testing.T) {
	t.Parallel()

	lister := &fakeActiveStateSnapshotScopeLister{scopes: []PendingConfigStateDriftScope{
		{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-state-1"},
		{ScopeID: "state_snapshot:s3:hash-2", GenerationID: "gen-state-2"},
	}}
	writer := &fakeCatchUpIntentWriter{result: IntentResult{Count: 2}}
	sweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer}

	got, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if got != 2 {
		t.Fatalf("RunOnce() = %d, want 2", got)
	}
	if len(writer.got) != 2 {
		t.Fatalf("Enqueue() called with %d intents, want 2", len(writer.got))
	}
	for i, intent := range writer.got {
		if intent.Domain != reducer.DomainConfigStateDrift {
			t.Fatalf("intent[%d].Domain = %q, want %q", i, intent.Domain, reducer.DomainConfigStateDrift)
		}
		if intent.Reason != configStateDriftCatchUpReason {
			t.Fatalf("intent[%d].Reason = %q, want %q", i, intent.Reason, configStateDriftCatchUpReason)
		}
		if intent.SourceSystem != configStateDriftCatchUpSourceSystem {
			t.Fatalf("intent[%d].SourceSystem = %q, want %q", i, intent.SourceSystem, configStateDriftCatchUpSourceSystem)
		}
	}
	if writer.got[0].ScopeID != "state_snapshot:s3:hash-1" || writer.got[0].GenerationID != "gen-state-1" {
		t.Fatalf("intent[0] = %#v, want scope/generation from the lister", writer.got[0])
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceIsNoOpWhenNoActiveScopes proves the
// sweeper never calls Enqueue with an empty slice.
func TestConfigStateDriftCatchUpSweeperRunOnceIsNoOpWhenNoActiveScopes(t *testing.T) {
	t.Parallel()

	lister := &fakeActiveStateSnapshotScopeLister{}
	writer := &fakeCatchUpIntentWriter{}
	sweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer}

	got, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if got != 0 {
		t.Fatalf("RunOnce() = %d, want 0", got)
	}
	if writer.got != nil {
		t.Fatalf("Enqueue() called with %#v, want no call", writer.got)
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceUsesConfiguredLimit proves Limit
// (and its default) reach the lister so a large corpus cannot turn every tick
// into an unbounded scan.
func TestConfigStateDriftCatchUpSweeperRunOnceUsesConfiguredLimit(t *testing.T) {
	t.Parallel()

	lister := &fakeActiveStateSnapshotScopeLister{}
	writer := &fakeCatchUpIntentWriter{}

	defaultSweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer}
	if _, err := defaultSweeper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	customSweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer, Limit: 42}
	if _, err := customSweeper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	if len(lister.calls) != 2 {
		t.Fatalf("lister called %d times, want 2", len(lister.calls))
	}
	if lister.calls[0] != defaultConfigStateDriftCatchUpLimit {
		t.Fatalf("default limit call = %d, want %d", lister.calls[0], defaultConfigStateDriftCatchUpLimit)
	}
	if lister.calls[1] != 42 {
		t.Fatalf("custom limit call = %d, want 42", lister.calls[1])
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceSurfacesListerError proves a
// lister failure is returned, not swallowed.
func TestConfigStateDriftCatchUpSweeperRunOnceSurfacesListerError(t *testing.T) {
	t.Parallel()

	lister := &fakeActiveStateSnapshotScopeLister{err: errors.New("db unavailable")}
	sweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: &fakeCatchUpIntentWriter{}}

	if _, err := sweeper.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want non-nil")
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceSurfacesEnqueueError proves an
// Enqueue failure is returned, not swallowed.
func TestConfigStateDriftCatchUpSweeperRunOnceSurfacesEnqueueError(t *testing.T) {
	t.Parallel()

	lister := &fakeActiveStateSnapshotScopeLister{scopes: []PendingConfigStateDriftScope{
		{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-state-1"},
	}}
	writer := &fakeCatchUpIntentWriter{err: errors.New("enqueue boom")}
	sweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer}

	if _, err := sweeper.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil, want non-nil")
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceIsNilSafe proves the sweeper is a
// pure no-op (never panics) when either dependency is unwired.
func TestConfigStateDriftCatchUpSweeperRunOnceIsNilSafe(t *testing.T) {
	t.Parallel()

	var sweeper ConfigStateDriftCatchUpSweeper
	got, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}
	if got != 0 {
		t.Fatalf("RunOnce() = %d, want 0", got)
	}
}

// TestConfigStateDriftCatchUpSweeperRunOnceRecordsEnqueueCounterByActualCount
// proves the sweeper's own CorrelationDriftIntentsEnqueued contribution uses
// the DB's actual insertion count (IntentResult.Count), labeled with
// source=reducer_catch_up_sweep, and stays silent when Instruments is nil.
func TestConfigStateDriftCatchUpSweeperRunOnceRecordsEnqueueCounterByActualCount(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	meter := metric.NewMeterProvider(metric.WithReader(reader)).Meter("test")
	inst, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}

	lister := &fakeActiveStateSnapshotScopeLister{scopes: []PendingConfigStateDriftScope{
		{ScopeID: "state_snapshot:s3:hash-1", GenerationID: "gen-state-1"},
		{ScopeID: "state_snapshot:s3:hash-2", GenerationID: "gen-state-2"},
	}}
	// Only 1 of the 2 attempted rows was actually inserted -- the other was
	// already enqueued by one of the other two producers.
	writer := &fakeCatchUpIntentWriter{result: IntentResult{Count: 1}}
	sweeper := ConfigStateDriftCatchUpSweeper{Active: lister, Intents: writer, Instruments: inst}

	if _, err := sweeper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	metricName := "eshu_dp_correlation_drift_intents_enqueued_total"
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				source, _ := dp.Attributes.Value(attribute.Key("source"))
				if source.AsString() == "reducer_catch_up_sweep" {
					found = true
					if dp.Value != 1 {
						t.Fatalf("%s{source=reducer_catch_up_sweep} = %d, want 1 (2 attempted, 1 actually inserted)", metricName, dp.Value)
					}
				}
			}
		}
	}
	if !found {
		t.Fatalf("%s{source=reducer_catch_up_sweep} series not found", metricName)
	}
}
