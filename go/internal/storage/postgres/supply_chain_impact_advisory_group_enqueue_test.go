// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type fakeAdvisoryGroupIntentQueue struct {
	enqueued  []runtime.ReducerIntent
	retouched []string
	enqErr    error
	reErr     error
}

func (f *fakeAdvisoryGroupIntentQueue) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	if f.enqErr != nil {
		return runtime.IntentResult{}, f.enqErr
	}
	f.enqueued = append(f.enqueued, intents...)
	return runtime.IntentResult{Count: len(intents)}, nil
}

func (f *fakeAdvisoryGroupIntentQueue) RetouchWorkItem(_ context.Context, workItemID string) (bool, error) {
	if f.reErr != nil {
		return false, f.reErr
	}
	f.retouched = append(f.retouched, workItemID)
	return true, nil
}

func advisoryGroupOriginIntent() reducercontract.Intent {
	return reducercontract.Intent{
		ScopeID:      "scope:vuln",
		GenerationID: "gen:vuln",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
	}
}

func TestAdvisoryGroupProjectionEnqueuerBuildsOneIntentPerGroup(t *testing.T) {
	t.Parallel()
	queue := &fakeAdvisoryGroupIntentQueue{}
	enqueuer := AdvisoryGroupProjectionEnqueuer{queue: queue}

	err := enqueuer.EnqueueAdvisoryGroupProjections(
		context.Background(), advisoryGroupOriginIntent(),
		[]string{"CVE-2026-0001", "GHSA-solo", "CVE-2026-0001", " ", ""},
	)
	if err != nil {
		t.Fatalf("EnqueueAdvisoryGroupProjections: %v", err)
	}
	if len(queue.enqueued) != 2 {
		t.Fatalf("enqueued %d intents, want 2 (deduped, blanks dropped)", len(queue.enqueued))
	}
	first := queue.enqueued[0]
	if first.Domain != reducercontract.DomainAdvisoryGroupProjection {
		t.Fatalf("domain = %q", first.Domain)
	}
	// Re-homed (#10566): every touch rides the synthetic slot, never the
	// origin pass's scope/generation, so one live row per group caps the
	// backlog at the group count.
	if first.ScopeID != AdvisoryGroupsSyntheticScope || first.GenerationID != AdvisoryGroupsSyntheticGenesis {
		t.Fatalf("scope/generation = %q/%q, want the synthetic slot %q/%q", first.ScopeID, first.GenerationID, AdvisoryGroupsSyntheticScope, AdvisoryGroupsSyntheticGenesis)
	}
	if first.SourceSystem != "vulnerability_intelligence" {
		t.Fatalf("source system = %q, want the origin's mirrored", first.SourceSystem)
	}
	if first.EntityKey != advisorygroup.EntityKey("CVE-2026-0001") {
		t.Fatalf("entity key = %q", first.EntityKey)
	}
	if got := first.Payload["group_id"]; got != "CVE-2026-0001" {
		t.Fatalf("payload group_id = %v", got)
	}
	// Drill-down only: the origin rides the payload and affects no key,
	// predicate, or handler read.
	if got := first.Payload["origin_scope_id"]; got != "scope:vuln" {
		t.Fatalf("payload origin_scope_id = %v, want scope:vuln", got)
	}
	if got := first.Payload["origin_generation_id"]; got != "gen:vuln" {
		t.Fatalf("payload origin_generation_id = %v, want gen:vuln", got)
	}
	if first.Reason == "" {
		t.Fatal("reason must name the cause, it becomes the claimed intent's Cause")
	}
	// Every enqueued intent is also retouched, so a group projected earlier
	// in this generation (succeeded row, same work item id) re-runs.
	wantRetouched := []string{
		reducerWorkItemID(queue.enqueued[0]),
		reducerWorkItemID(queue.enqueued[1]),
	}
	if !reflect.DeepEqual(queue.retouched, wantRetouched) {
		t.Fatalf("retouched = %v, want %v", queue.retouched, wantRetouched)
	}
	// The slot is a pure function of the group: the same group touched
	// from a disjoint origin collides with this intent instead of
	// admitting a second row.
	other := advisoryGroupOriginIntent()
	other.ScopeID = "scope:other"
	other.GenerationID = "gen:other"
	shadow := &fakeAdvisoryGroupIntentQueue{}
	shadowEnqueuer := AdvisoryGroupProjectionEnqueuer{queue: shadow}
	if err := shadowEnqueuer.EnqueueAdvisoryGroupProjections(context.Background(), other, []string{"CVE-2026-0001"}); err != nil {
		t.Fatalf("shadow enqueue: %v", err)
	}
	if len(shadow.enqueued) != 1 {
		t.Fatalf("shadow enqueued %d intents, want 1", len(shadow.enqueued))
	}
	if reducerWorkItemID(shadow.enqueued[0]) != reducerWorkItemID(queue.enqueued[0]) {
		t.Fatalf("origin-scoped touch diverges: %q vs %q", reducerWorkItemID(shadow.enqueued[0]), reducerWorkItemID(queue.enqueued[0]))
	}
}

// TestAdvisoryGroupProjectionEnqueuerStampsFirstUnderlyingDataAt pins the
// Decision-3 (Q1 option a, eshu-hq/eshu#11080) wire write: the projection
// intent carries the touching pass's cycle start (its enqueue when the
// cycle start is zero) as the first-underlying-data time the handler's
// stale fast path anchors on. An origin with no times at all (the ifa
// drive verb's hand-built origin) stamps nothing: the handler falls back
// to the post-claim deferral clock.
func TestAdvisoryGroupProjectionEnqueuerStampsFirstUnderlyingDataAt(t *testing.T) {
	t.Parallel()
	cycleStart := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

	origin := advisoryGroupOriginIntent()
	origin.EnqueuedAt = cycleStart.Add(-time.Hour)
	origin.CycleStartedAt = cycleStart
	queue := &fakeAdvisoryGroupIntentQueue{}
	enqueuer := AdvisoryGroupProjectionEnqueuer{queue: queue}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(context.Background(), origin, []string{"CVE-2026-0001"}); err != nil {
		t.Fatalf("EnqueueAdvisoryGroupProjections: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d intents, want 1", len(queue.enqueued))
	}
	got, ok := queue.enqueued[0].Payload[advisorygroup.PayloadKeyFirstUnderlyingDataAt]
	if !ok {
		t.Fatalf("payload %q is absent, want the origin cycle start stamped", advisorygroup.PayloadKeyFirstUnderlyingDataAt)
	}
	stamped, ok := got.(string)
	if !ok {
		t.Fatalf("payload %q = %T, want an RFC3339Nano string", advisorygroup.PayloadKeyFirstUnderlyingDataAt, got)
	}
	parsed, err := time.Parse(time.RFC3339Nano, stamped)
	if err != nil {
		t.Fatalf("payload %q = %q, want RFC3339Nano: %v", advisorygroup.PayloadKeyFirstUnderlyingDataAt, stamped, err)
	}
	if !parsed.Equal(cycleStart) {
		t.Fatalf("stamped first-underlying-data = %v, want the origin cycle start %v", parsed, cycleStart)
	}

	// A pass that only carries its enqueue stamps the enqueue.
	enqueueOnly := advisoryGroupOriginIntent()
	enqueueOnly.EnqueuedAt = cycleStart
	enqueueOnlyQueue := &fakeAdvisoryGroupIntentQueue{}
	enqueueOnlyEnqueuer := AdvisoryGroupProjectionEnqueuer{queue: enqueueOnlyQueue}
	if err := enqueueOnlyEnqueuer.EnqueueAdvisoryGroupProjections(context.Background(), enqueueOnly, []string{"CVE-2026-0001"}); err != nil {
		t.Fatalf("EnqueueAdvisoryGroupProjections: %v", err)
	}
	got, ok = enqueueOnlyQueue.enqueued[0].Payload[advisorygroup.PayloadKeyFirstUnderlyingDataAt]
	if !ok {
		t.Fatalf("payload %q is absent, want the origin enqueue stamped", advisorygroup.PayloadKeyFirstUnderlyingDataAt)
	}
	parsed, err = time.Parse(time.RFC3339Nano, got.(string))
	if err != nil || !parsed.Equal(cycleStart) {
		t.Fatalf("stamped first-underlying-data = %v (%v), want the origin enqueue %v", got, err, cycleStart)
	}

	// No origin times at all stamps nothing: the handler falls back.
	timeless := advisoryGroupOriginIntent()
	timelessQueue := &fakeAdvisoryGroupIntentQueue{}
	timelessEnqueuer := AdvisoryGroupProjectionEnqueuer{queue: timelessQueue}
	if err := timelessEnqueuer.EnqueueAdvisoryGroupProjections(context.Background(), timeless, []string{"CVE-2026-0001"}); err != nil {
		t.Fatalf("EnqueueAdvisoryGroupProjections: %v", err)
	}
	if _, ok := timelessQueue.enqueued[0].Payload[advisorygroup.PayloadKeyFirstUnderlyingDataAt]; ok {
		t.Fatalf("payload %q is stamped from a timeless origin, want it absent so the handler falls back", advisorygroup.PayloadKeyFirstUnderlyingDataAt)
	}
}

func TestAdvisoryGroupProjectionEnqueuerEmptyAndErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	boom := errors.New("boom")

	queue := &fakeAdvisoryGroupIntentQueue{}
	enqueuer := AdvisoryGroupProjectionEnqueuer{queue: queue}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(ctx, advisoryGroupOriginIntent(), nil); err != nil {
		t.Fatalf("empty group set: %v", err)
	}
	if len(queue.enqueued) != 0 {
		t.Fatal("empty group set enqueued intents")
	}

	queue = &fakeAdvisoryGroupIntentQueue{enqErr: boom}
	enqueuer = AdvisoryGroupProjectionEnqueuer{queue: queue}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(ctx, advisoryGroupOriginIntent(), []string{"CVE-1"}); !errors.Is(err, boom) {
		t.Fatalf("enqueue error = %v, want wrapped boom", err)
	}
	if len(queue.retouched) != 0 {
		t.Fatal("reopen ran after a failed enqueue")
	}

	queue = &fakeAdvisoryGroupIntentQueue{reErr: boom}
	enqueuer = AdvisoryGroupProjectionEnqueuer{queue: queue}
	if err := enqueuer.EnqueueAdvisoryGroupProjections(ctx, advisoryGroupOriginIntent(), []string{"CVE-1"}); !errors.Is(err, boom) {
		t.Fatalf("reopen error = %v, want wrapped boom", err)
	}
}
