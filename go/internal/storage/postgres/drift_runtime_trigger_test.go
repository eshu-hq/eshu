// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TestConfigStateDriftRuntimeTriggerEnqueuesOneIntentForActivatedGeneration
// proves the runtime trigger (issue #5593) writes exactly the reducer intent
// shape IngestionStore.EnqueueConfigStateDriftIntents already uses for its
// bootstrap Phase 3.5 sweep, over the real ReducerQueue.Enqueue SQL path, and
// advances CorrelationDriftIntentsEnqueued with the new
// ingester_runtime_trigger source label.
func TestConfigStateDriftRuntimeTriggerEnqueuesOneIntentForActivatedGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	inst, reader := newEnqueueInstruments(t)
	trigger := ConfigStateDriftRuntimeTrigger{
		Queue:       ReducerQueue{db: db},
		Instruments: inst,
	}

	if err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1"); err != nil {
		t.Fatalf("TriggerConfigStateDrift() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (single batch INSERT)", got, want)
	}
	insert := db.execs[0].query
	if !strings.Contains(insert, "INSERT INTO fact_work_items") {
		t.Fatalf("exec query missing fact_work_items insert: %s", insert)
	}
	if !strings.Contains(insert, "ON CONFLICT (work_item_id) DO NOTHING") {
		t.Fatalf("exec query missing ON CONFLICT DO NOTHING dedupe: %s", insert)
	}
	foundDomain, foundScope, foundGeneration := false, false, false
	for _, arg := range db.execs[0].args {
		s, ok := arg.(string)
		if !ok {
			continue
		}
		switch s {
		case "config_state_drift":
			foundDomain = true
		case "state_snapshot:s3:hash-1":
			foundScope = true
		case "gen-state-1":
			foundGeneration = true
		}
	}
	if !foundDomain || !foundScope || !foundGeneration {
		t.Fatalf("expected domain/scope_id/generation_id in INSERT args, got %#v", db.execs[0].args)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got, want := counterTotal(rm, "eshu_dp_correlation_drift_intents_enqueued_total"), int64(1); got != want {
		t.Fatalf("enqueue counter = %d, want %d", got, want)
	}
	assertCounterPresentWithLabels(t, rm, "eshu_dp_correlation_drift_intents_enqueued_total", map[string]string{
		"pack":   "terraform_config_state_drift",
		"source": "ingester_runtime_trigger",
	})
}

// TestConfigStateDriftRuntimeTriggerAndBootstrapProduceSameConflictKey proves
// the ON CONFLICT semantics the issue asks us to prove, not assume: the
// runtime trigger and the bootstrap Phase 3.5 sweep (listActiveStateSnapshotScopes)
// build reducer intents that hash to the IDENTICAL work_item_id for the same
// (scope, generation) pair. That is what makes the two producers safely
// idempotent against each other (whichever fires first wins the row; the
// second is a harmless ON CONFLICT DO NOTHING no-op) rather than silently
// racing to write different rows.
func TestConfigStateDriftRuntimeTriggerAndBootstrapProduceSameConflictKey(t *testing.T) {
	t.Parallel()

	scopeID := "state_snapshot:s3:hash-1"
	generationID := "gen-state-1"

	bootstrapIntent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       "config_state_drift",
		Reason:       driftIntentReason,
		SourceSystem: driftIntentSourceSystem,
	}
	runtimeIntent := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       "config_state_drift",
		Reason:       driftRuntimeTriggerReason,
		SourceSystem: driftRuntimeTriggerSourceSystem,
	}

	if got, want := reducerWorkItemID(runtimeIntent), reducerWorkItemID(bootstrapIntent); got != want {
		t.Fatalf("runtime trigger work_item_id = %q, want %q (must match bootstrap's so the two producers dedupe against each other)", got, want)
	}
}

// TestConfigStateDriftRuntimeTriggerWrapsEnqueueError proves a downstream
// enqueue failure is surfaced to the caller (ProjectorQueue's hook is
// responsible for swallowing it, not this type) rather than silently
// dropped.
func TestConfigStateDriftRuntimeTriggerWrapsEnqueueError(t *testing.T) {
	t.Parallel()

	trigger := ConfigStateDriftRuntimeTrigger{Queue: failingReducerIntentWriter{err: errors.New("db unavailable")}}

	err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1")
	if err == nil {
		t.Fatal("TriggerConfigStateDrift() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "db unavailable") {
		t.Fatalf("error = %v, want it to wrap the underlying enqueue error", err)
	}
}

// TestConfigStateDriftRuntimeTriggerRequiresQueue proves the trigger fails
// closed instead of panicking when constructed without a Queue.
func TestConfigStateDriftRuntimeTriggerRequiresQueue(t *testing.T) {
	t.Parallel()

	var trigger ConfigStateDriftRuntimeTrigger
	if err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1"); err == nil {
		t.Fatal("nil Queue: error = nil, want non-nil")
	}
}

type failingReducerIntentWriter struct {
	err error
}

func (f failingReducerIntentWriter) Enqueue(context.Context, []projector.ReducerIntent) (projector.IntentResult, error) {
	return projector.IntentResult{}, f.err
}

// recordingRedriveScheduler captures every EnsureScheduled call for
// assertion, standing in for ConfigStateDriftRedriveStore.
type recordingRedriveScheduler struct {
	calls []struct {
		scopeID        string
		generationID   string
		firstAttemptAt time.Time
	}
	err error
}

func (r *recordingRedriveScheduler) EnsureScheduled(_ context.Context, scopeID, generationID string, firstAttemptAt time.Time) error {
	r.calls = append(r.calls, struct {
		scopeID        string
		generationID   string
		firstAttemptAt time.Time
	}{scopeID, generationID, firstAttemptAt})
	return r.err
}

// TestConfigStateDriftRuntimeTriggerSchedulesRedriveAfterEnqueue proves the
// issue #5593 P1-1 wiring: after a successful enqueue, the trigger schedules
// a bounded redrive for the SAME (scope, generation) at now + RedriveDelay,
// so a premature "no config repo owns this backend" outcome is not left
// permanently terminal.
func TestConfigStateDriftRuntimeTriggerSchedulesRedriveAfterEnqueue(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	scheduler := &recordingRedriveScheduler{}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	trigger := ConfigStateDriftRuntimeTrigger{
		Queue:        ReducerQueue{db: db},
		Redrive:      scheduler,
		RedriveDelay: 5 * time.Minute,
		Now:          func() time.Time { return now },
	}

	if err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1"); err != nil {
		t.Fatalf("TriggerConfigStateDrift() error = %v, want nil", err)
	}

	if len(scheduler.calls) != 1 {
		t.Fatalf("EnsureScheduled call count = %d, want 1", len(scheduler.calls))
	}
	call := scheduler.calls[0]
	if call.scopeID != "state_snapshot:s3:hash-1" || call.generationID != "gen-state-1" {
		t.Fatalf("EnsureScheduled args = %+v, want scope/generation to match the enqueued intent", call)
	}
	if want := now.Add(5 * time.Minute); !call.firstAttemptAt.Equal(want) {
		t.Fatalf("EnsureScheduled firstAttemptAt = %v, want %v (now + RedriveDelay)", call.firstAttemptAt, want)
	}
}

// TestConfigStateDriftRuntimeTriggerSkipsRedriveSchedulingWhenNil proves the
// scheduler is a pure no-op (never called, never panics) when Redrive is
// unwired -- every caller that predates issue #5593 P1-1's fix.
func TestConfigStateDriftRuntimeTriggerSkipsRedriveSchedulingWhenNil(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	trigger := ConfigStateDriftRuntimeTrigger{Queue: ReducerQueue{db: db}}

	if err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1"); err != nil {
		t.Fatalf("TriggerConfigStateDrift() error = %v, want nil", err)
	}
}

// TestConfigStateDriftRuntimeTriggerSurfacesRedriveSchedulingError proves a
// redrive-scheduling failure is reported to the caller (so
// runConfigStateDriftTriggerHook logs it) even though the primary enqueue
// already succeeded.
func TestConfigStateDriftRuntimeTriggerSurfacesRedriveSchedulingError(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	scheduler := &recordingRedriveScheduler{err: errors.New("ledger write failed")}
	trigger := ConfigStateDriftRuntimeTrigger{
		Queue:   ReducerQueue{db: db},
		Redrive: scheduler,
	}

	err := trigger.TriggerConfigStateDrift(context.Background(), "state_snapshot:s3:hash-1", "gen-state-1")
	if err == nil {
		t.Fatal("TriggerConfigStateDrift() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ledger write failed") {
		t.Fatalf("error = %v, want it to wrap the redrive scheduling error", err)
	}
	// The enqueue itself must still have gone through -- one batch INSERT.
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d (enqueue must succeed even though scheduling failed)", got, want)
	}
}
