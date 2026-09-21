// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// opaqueAckBatchSink returns an error that does not wrap
// reducer.ErrExecutionClaimRejected, which is the shape
// splitReducerAckBatchIntents produced for a lease reclaim before #6162.
type opaqueAckBatchSink struct {
	mu       sync.Mutex
	ackCalls int
	ackErr   error
}

func (s *opaqueAckBatchSink) Ack(context.Context, reducer.Intent, reducer.Result) error {
	return nil
}

func (s *opaqueAckBatchSink) AckBatch(context.Context, []reducer.Intent, []reducer.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCalls++
	return s.ackErr
}

func (s *opaqueAckBatchSink) Fail(context.Context, reducer.Intent, error) error {
	return nil
}

// The seeded-violation half of the pair whose passing half is
// TestServiceBatchStaleAckDoesNotStopReducer.
//
// The batch acker branches on errors.Is(err, ErrExecutionClaimRejected): the
// claim-rejected branch logs and keeps polling, every other error calls
// appendErr, which cancels the run context and stops the reducer. That is what
// made the Ifa expire-lease cell hang in run 35617012182 — a lease reclaim
// produced a duplicate ack batch, the split rejected it with a plain error, and
// the reducer exited with the row still 'claimed'.
//
// Pinning the fatal branch here is what gives the postgres-side change its
// meaning: wrapping reducer.ErrExecutionClaimRejected is not cosmetic, it is
// the difference between a logged lease race and a dead reducer.
func TestServiceBatchOpaqueAckErrorStopsReducer(t *testing.T) {
	t.Parallel()

	opaque := errors.New(
		`batch ack reducer work item "duplicate-claim-ack" has conflicting domains`,
	)
	intent := reducer.Intent{
		IntentID:     "duplicate-claim-ack",
		ScopeID:      "scope-duplicate-claim-ack",
		GenerationID: "generation-duplicate-claim-ack",
		Domain:       reducer.DomainRepoDependency,
		AvailableAt:  time.Now().UTC(),
	}
	source := &staleAckBatchSource{intent: &intent}
	sink := &opaqueAckBatchSink{ackErr: opaque}
	service := reducer.Service{
		PollInterval:   time.Millisecond,
		WorkSource:     source,
		Executor:       staleAckExecutor{},
		WorkSink:       sink,
		Workers:        2,
		BatchClaimSize: 1,
		Wait: func(context.Context, time.Duration) error {
			return context.Canceled
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := service.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want the opaque batch ACK error to stop the reducer")
	}
	if !errors.Is(err, opaque) {
		t.Fatalf("Run() error = %v, want it to carry the opaque batch ACK error", err)
	}
	if errors.Is(err, reducer.ErrExecutionClaimRejected) {
		t.Fatal("the opaque control must not be a claim rejection; it would no longer seed the fatal branch")
	}
	sink.mu.Lock()
	ackCalls := sink.ackCalls
	sink.mu.Unlock()
	if ackCalls != 1 {
		t.Fatalf("AckBatch() calls = %d, want 1", ackCalls)
	}
}
