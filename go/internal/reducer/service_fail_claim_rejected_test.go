// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// failRejectedByLostClaimSink rejects WorkSink.Fail the way ReducerQueue.Fail
// does for a claim another worker already reclaimed: zero rows match the lease
// fence, so the error wraps ErrExecutionClaimRejected.
type failRejectedByLostClaimSink struct {
	stubReducerWorkSink
}

func (s *failRejectedByLostClaimSink) Fail(ctx context.Context, intent Intent, err error) error {
	_ = s.stubReducerWorkSink.Fail(ctx, intent, err)
	return fmt.Errorf("reducer work claim rejected: %w", ErrExecutionClaimRejected)
}

type erroringReducerExecutor struct{ err error }

func (e erroringReducerExecutor) Execute(context.Context, Intent) (Result, error) {
	return Result{}, e.err
}

// TestServiceRunTreatsAFailRejectedByALostClaimAsBenign pins the stale-worker
// path of #7142. A frozen worker resumes after its item was reclaimed and
// another worker finished it; its supply-chain impact write is rejected as
// superseded, the handler returns that error, and the service calls
// WorkSink.Fail. The lease fence rejects that Fail too, because the claim is no
// longer this worker's. The rejection means "this claim is lost", exactly as a
// lost heartbeat does, so the worker must drop the item and keep draining. A
// Fail error that stops the run would let one ordinary lease race terminate the
// reducer process even though the stale write itself was fenced.
func TestServiceRunTreatsAFailRejectedByALostClaimAsBenign(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-stale-fail",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}
	sink := &failRejectedByLostClaimSink{}

	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:     erroringReducerExecutor{err: errors.New("supply chain impact write superseded")},
		WorkSink:     sink,
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}

	if err := service.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil: a Fail rejected by a lost claim must not stop the reducer", err)
	}
	if sink.failCalls != 1 {
		t.Fatalf("WorkSink.Fail calls = %d, want 1", sink.failCalls)
	}
}

// TestServiceRunBatchTreatsAFailRejectedByALostClaimAsBenign is the same
// contract for the batch path (executeAndReport), which returns a Fail error to
// runBatchConcurrent, where it cancels the run for every worker.
func TestServiceRunBatchTreatsAFailRejectedByALostClaimAsBenign(t *testing.T) {
	t.Parallel()

	intent := failedAckTestIntent("intent-stale-fail", DomainCICDRunCorrelation)
	source := &fakeBatchWorkSource{intents: []Intent{intent}}
	sink := &batchFailRejectedByLostClaimSink{}

	svc := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     source,
		Executor:       erroringReducerExecutor{err: errors.New("supply chain impact write superseded")},
		WorkSink:       sink,
		Workers:        4,
		BatchClaimSize: 8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := svc.runMainLoop(ctx); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil: a Fail rejected by a lost claim must not stop the reducer", err)
	}
	sink.mu.Lock()
	failed := len(sink.failedBy)
	sink.mu.Unlock()
	if failed != 1 {
		t.Fatalf("Fail() calls = %d, want 1", failed)
	}
}

type batchFailRejectedByLostClaimSink struct {
	fakeBatchWorkSink
}

func (s *batchFailRejectedByLostClaimSink) Fail(ctx context.Context, intent Intent, err error) error {
	_ = s.fakeBatchWorkSink.Fail(ctx, intent, err)
	return fmt.Errorf("reducer work claim rejected: %w", ErrExecutionClaimRejected)
}

// brokenFailSink returns a plain sink error from Fail: unlike a lost claim, this
// is a real failure to terminalize the item and must still stop the run.
type brokenFailSink struct {
	stubReducerWorkSink
}

func (s *brokenFailSink) Fail(ctx context.Context, intent Intent, err error) error {
	_ = s.stubReducerWorkSink.Fail(ctx, intent, err)
	return errors.New("postgres unavailable")
}

type batchBrokenFailSink struct {
	fakeBatchWorkSink
}

func (s *batchBrokenFailSink) Fail(ctx context.Context, intent Intent, err error) error {
	_ = s.fakeBatchWorkSink.Fail(ctx, intent, err)
	return errors.New("postgres unavailable")
}

// TestServiceRunStillFailsOnABrokenFailSink is the negative control for the
// lost-claim tests above: only a Fail rejected by a lost claim is benign. A
// plain sink error must still be returned, so swallowing every Fail error would
// turn this red.
func TestServiceRunStillFailsOnABrokenFailSink(t *testing.T) {
	t.Parallel()

	intent := Intent{
		IntentID:     "intent-broken-fail",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}
	service := Service{
		PollInterval: 10 * time.Millisecond,
		WorkSource:   &stubReducerWorkSource{intents: []Intent{intent}},
		Executor:     erroringReducerExecutor{err: errors.New("handler failed")},
		WorkSink:     &brokenFailSink{},
		Wait:         func(context.Context, time.Duration) error { return context.Canceled },
	}
	err := service.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fail reducer work") {
		t.Fatalf("Run() error = %v, want it to carry %q for a broken Fail sink", err, "fail reducer work")
	}
}

// TestServiceRunBatchStillFailsOnABrokenFailSink is the same control for the
// batch path.
func TestServiceRunBatchStillFailsOnABrokenFailSink(t *testing.T) {
	t.Parallel()

	svc := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     &fakeBatchWorkSource{intents: []Intent{failedAckTestIntent("intent-broken-fail", DomainCICDRunCorrelation)}},
		Executor:       erroringReducerExecutor{err: errors.New("handler failed")},
		WorkSink:       &batchBrokenFailSink{},
		Workers:        4,
		BatchClaimSize: 8,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := svc.runMainLoop(ctx)
	if err == nil || !strings.Contains(err.Error(), "fail reducer work") {
		t.Fatalf("runMainLoop() error = %v, want it to carry %q for a broken Fail sink", err, "fail reducer work")
	}
}
