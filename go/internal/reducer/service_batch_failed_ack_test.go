// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"
	"time"
)

// deferringExecutor stands in for a cross-scope consumer whose declared
// producer has not activated yet -- the normal, expected transient failure
// logged as "cross-scope consumer deferred: producer scopes have not
// activated" (cross_scope_readiness_floor.go). The handler returns an error,
// so the item is terminalized by WorkSink.Fail and retried later.
type deferringExecutor struct{}

func (deferringExecutor) Execute(_ context.Context, _ Intent) (Result, error) {
	return Result{}, errors.New("cross-scope consumer deferred: producer scopes have not activated")
}

func failedAckTestIntent(id string, domain Domain) Intent {
	return Intent{
		IntentID:        id,
		ScopeID:         "ci_cd_run:github_actions:eshu-hq:supply-chain-demo",
		GenerationID:    "gen-1",
		SourceSystem:    "github_actions",
		Domain:          domain,
		Cause:           "test",
		EntityKeys:      []string{"key-" + id},
		RelatedScopeIDs: []string{"scope-1"},
		Status:          IntentStatusClaimed,
		EnqueuedAt:      time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
	}
}

// TestServiceRunBatchDoesNotAckIntentsItAlreadyFailed pins the batch path to
// the same contract the per-item path (service.go reduceOnce) already holds:
// an intent whose handler returned an error is terminalized by WorkSink.Fail
// and MUST NOT also be handed to AckBatch.
//
// The regression this catches killed the whole reducer process, not just the
// item. Fail() sets status='retrying' and lease_owner=NULL
// (retryReducerWorkQuery). A later ack of that same intent matches
// `lease_owner = $2 AND status IN ('claimed','running')` on zero rows, and for
// the container_image_identity and ci_cd_run_correlation domains ReducerQueue
// .AckBatch reads rowsAffected==0 as ErrReducerClaimRejected. That error
// reaches appendErr in runBatchConcurrent, which cancels the run context, so
// every in-flight worker aborts with "context canceled" and cmd/reducer exits.
// A cross-scope readiness deferral is routine, so this turned a normal retry
// into a reducer that stops draining for the rest of the run.
func TestServiceRunBatchDoesNotAckIntentsItAlreadyFailed(t *testing.T) {
	t.Parallel()

	for _, domain := range []Domain{
		DomainCICDRunCorrelation,
		DomainContainerImageIdentity,
		DomainCodeCallMaterialization,
	} {
		t.Run(string(domain), func(t *testing.T) {
			t.Parallel()

			intent := failedAckTestIntent("intent-failed-1", domain)
			source := &fakeBatchWorkSource{intents: []Intent{intent}}
			sink := &fakeBatchWorkSink{}

			svc := Service{
				PollInterval:   10 * time.Millisecond,
				WorkSource:     source,
				Executor:       deferringExecutor{},
				WorkSink:       sink,
				Workers:        4,
				BatchClaimSize: 8,
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if err := svc.runMainLoop(ctx); err != nil {
				t.Fatalf("runMainLoop() error = %v, want nil", err)
			}

			sink.mu.Lock()
			ackIDs := append([]string(nil), sink.ackIDs...)
			failedBy := append([]string(nil), sink.failedBy...)
			sink.mu.Unlock()

			if len(failedBy) != 1 || failedBy[0] != intent.IntentID {
				t.Fatalf("Fail() intent ids = %v, want [%s]", failedBy, intent.IntentID)
			}
			if len(ackIDs) != 0 {
				t.Fatalf("AckBatch() intent ids = %v, want none: a failed intent must not be acked", ackIDs)
			}
		})
	}
}

// TestServiceRunBatchStillAcksSucceededSiblingsOfAFailedIntent keeps the fix
// narrow: skipping the failed intent must not drop the acks of the items that
// did succeed in the same claim batch.
func TestServiceRunBatchStillAcksSucceededSiblingsOfAFailedIntent(t *testing.T) {
	t.Parallel()

	failing := failedAckTestIntent("intent-failing", DomainCICDRunCorrelation)
	succeeding := failedAckTestIntent("intent-succeeding", DomainCICDRunCorrelation)

	source := &fakeBatchWorkSource{intents: []Intent{failing, succeeding}}
	sink := &fakeBatchWorkSink{}

	svc := Service{
		PollInterval:   10 * time.Millisecond,
		WorkSource:     source,
		Executor:       selectivelyFailingExecutor{failIntentID: failing.IntentID},
		WorkSink:       sink,
		Workers:        4,
		BatchClaimSize: 8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := svc.runMainLoop(ctx); err != nil {
		t.Fatalf("runMainLoop() error = %v, want nil", err)
	}

	sink.mu.Lock()
	ackIDs := append([]string(nil), sink.ackIDs...)
	failedBy := append([]string(nil), sink.failedBy...)
	sink.mu.Unlock()

	if len(failedBy) != 1 || failedBy[0] != failing.IntentID {
		t.Fatalf("Fail() intent ids = %v, want [%s]", failedBy, failing.IntentID)
	}
	if len(ackIDs) != 1 || ackIDs[0] != succeeding.IntentID {
		t.Fatalf("AckBatch() intent ids = %v, want [%s]", ackIDs, succeeding.IntentID)
	}
}

type selectivelyFailingExecutor struct {
	failIntentID string
}

func (e selectivelyFailingExecutor) Execute(_ context.Context, intent Intent) (Result, error) {
	if intent.IntentID == e.failIntentID {
		return Result{}, errors.New("cross-scope consumer deferred: producer scopes have not activated")
	}
	return Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   ResultStatusSucceeded,
	}, nil
}
