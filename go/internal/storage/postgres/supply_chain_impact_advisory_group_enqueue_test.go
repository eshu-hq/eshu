// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"

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
	if first.ScopeID != "scope:vuln" || first.GenerationID != "gen:vuln" {
		t.Fatalf("scope/generation = %q/%q, want the origin intent's", first.ScopeID, first.GenerationID)
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
