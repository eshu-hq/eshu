// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

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

// TestConfigStateDriftRuntimeTriggerDistinctGenerationsProduceDistinctWorkItemIDs
// proves the load-bearing property the "race self-heals on the next
// terraform apply" argument depends on (issue #5593, see
// ConfigStateDriftRuntimeTrigger's doc comment and
// TerraformConfigStateDriftHandler's ErrNoConfigRepoOwnsBackend branch): two
// intents for the SAME scope and domain but DIFFERENT GenerationID hash to
// DIFFERENT work_item_id values, so a new state_snapshot generation (a new
// terraform apply bumps the embedded serial, per
// go/internal/scope/tfstate.go's GenerationID format
// "terraform_state:<scopeID>:<lineageUUID>:serial:<serial>") is never
// blocked by the OLD generation's ON CONFLICT DO NOTHING row -- it gets its
// own independent evaluation. This is the opposite property from
// TestConfigStateDriftRuntimeTriggerAndBootstrapProduceSameConflictKey
// above, which proves the SAME generation dedupes across producers; that
// test was previously (and wrongly) cited as proof of THIS property.
func TestConfigStateDriftRuntimeTriggerDistinctGenerationsProduceDistinctWorkItemIDs(t *testing.T) {
	t.Parallel()

	scopeID := "state_snapshot:s3:hash-1"
	lineage := "lineage-abc"

	firstApply := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: "terraform_state:" + scopeID + ":" + lineage + ":serial:1",
		Domain:       "config_state_drift",
		Reason:       driftRuntimeTriggerReason,
		SourceSystem: driftRuntimeTriggerSourceSystem,
	}
	secondApply := projector.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: "terraform_state:" + scopeID + ":" + lineage + ":serial:2",
		Domain:       "config_state_drift",
		Reason:       driftRuntimeTriggerReason,
		SourceSystem: driftRuntimeTriggerSourceSystem,
	}

	firstID := reducerWorkItemID(firstApply)
	secondID := reducerWorkItemID(secondApply)
	if firstID == secondID {
		t.Fatalf("two intents for the same scope+domain but different GenerationID produced the SAME work_item_id (%q) -- a new terraform apply would be silently blocked by the prior generation's ON CONFLICT DO NOTHING row instead of getting its own evaluation", firstID)
	}
	if firstID == "" || secondID == "" {
		t.Fatalf("work_item_id must not be empty: first=%q second=%q", firstID, secondID)
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
