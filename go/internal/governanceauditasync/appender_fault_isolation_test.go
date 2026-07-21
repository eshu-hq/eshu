// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package governanceauditasync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// gatedValidatingSink mirrors GovernanceAuditStore.Append's all-or-nothing
// durability contract: it normalizes every event in the batch and returns an
// error on the FIRST that fails validation, persisting NONE of the batch —
// exactly the storage-layer behavior that turns one malformed event into a
// whole-batch loss unless the appender isolates it. The started/proceed gate
// lets a test park the single worker inside a blocked Append so it can
// deterministically fill the buffer and force the malformed event to share
// one flush batch with its well-formed siblings.
type gatedValidatingSink struct {
	mu       sync.Mutex
	received []governanceaudit.Event

	started chan struct{}
	proceed chan struct{}
}

func (s *gatedValidatingSink) Append(_ context.Context, events []governanceaudit.Event) error {
	if s.started != nil {
		select {
		case s.started <- struct{}{}:
		default:
		}
	}
	if s.proceed != nil {
		<-s.proceed
	}
	for _, event := range events {
		if _, err := governanceaudit.NormalizeEvent(event); err != nil {
			return fmt.Errorf("gatedValidatingSink: normalize: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, events...)
	return nil
}

func (s *gatedValidatingSink) receivedCorrelationIDs() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make(map[string]bool, len(s.received))
	for _, event := range s.received {
		ids[event.CorrelationID] = true
	}
	return ids
}

func (s *gatedValidatingSink) receivedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.received)
}

// malformedEvent is a scoped_token event with an EMPTY ActorIDHash: exactly
// the shape the F-9 P1 emission-site bug produced. It fails
// NormalizeEvent's actor_identity check, so the durable store rejects any
// batch containing it.
func malformedEvent(correlationID string) governanceaudit.Event {
	return governanceaudit.Event{
		Type:          governanceaudit.EventTypeReadAuthorization,
		ActorClass:    governanceaudit.ActorClassScopedToken,
		ActorIDHash:   "", // invalid: scoped_token requires an actor identity
		ScopeClass:    governanceaudit.ScopeClassAdmin,
		Decision:      governanceaudit.DecisionAllowed,
		ReasonCode:    "scoped_read_allowed",
		CorrelationID: correlationID,
		OccurredAt:    time.Now().UTC(),
	}
}

// TestAsyncAppender_MalformedEventDoesNotDropBatchSiblings is the F-9 (#5170)
// P1 defense-in-depth regression: one malformed event sharing a flush batch
// with N well-formed events must NOT take the whole batch down. Before the
// per-event fault-isolation fallback, a single batch Append error dropped the
// entire batch and counted PersistFailures += len(batch); the N well-formed
// allowed-read audit records were silently destroyed.
//
// The gate forces determinism: the worker parks in a blocked seed Append
// while the test fills the buffer with 1 malformed + N valid events, so the
// second flush drains them all into ONE batch that the sink rejects
// wholesale — the exact worst case.
func TestAsyncAppender_MalformedEventDoesNotDropBatchSiblings(t *testing.T) {
	t.Parallel()

	sink := &gatedValidatingSink{started: make(chan struct{}, 1), proceed: make(chan struct{})}
	metrics, reader := testMetrics(t)
	const validCount = 6
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(64))

	// Seed one valid event so the worker picks it up and blocks inside the
	// gated Append, parking the single worker and leaving the buffer free to
	// fill deterministically.
	if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent("seed")}); err != nil {
		t.Fatalf("Append() seed error = %v", err)
	}
	select {
	case <-sink.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for worker to enter the gated seed Append")
	}

	// Fill the buffer: the malformed event plus N well-formed siblings. All
	// sit in the buffer while the worker is parked, so the next flush drains
	// them into one batch.
	if err := appender.Append(context.Background(), []governanceaudit.Event{malformedEvent("malformed")}); err != nil {
		t.Fatalf("Append() malformed error = %v", err)
	}
	for i := 0; i < validCount; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("valid-%d", i))}); err != nil {
			t.Fatalf("Append() valid %d error = %v", i, err)
		}
	}

	// Release the gate so the worker flushes the seed, then drains and
	// flushes the malformed+valid batch, then Close drains any tail.
	close(sink.proceed)
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	got := sink.receivedCorrelationIDs()
	// Every well-formed event must have persisted despite sharing a batch
	// with the malformed one.
	for i := 0; i < validCount; i++ {
		id := fmt.Sprintf("valid-%d", i)
		if !got[id] {
			t.Errorf("well-formed event %q was not persisted — batch dropped wholesale", id)
		}
	}
	if !got["seed"] {
		t.Errorf("seed event was not persisted")
	}
	// The malformed event genuinely cannot persist and must be the only
	// failure counted (not its len(batch)-1 innocent siblings).
	if got["malformed"] {
		t.Errorf("malformed event unexpectedly persisted through the validating sink")
	}
	if gotCount, want := sink.receivedCount(), 1+validCount; gotCount != want {
		t.Errorf("persisted event count = %d, want %d (seed + %d valid, malformed isolated)", gotCount, want, validCount)
	}
	if gotFailures, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_persist_failures_total"), int64(1); gotFailures != want {
		t.Fatalf("persist failures = %d, want exactly %d (only the malformed event, not its batch siblings)", gotFailures, want)
	}
	// All 1+validCount+1 events were accepted into the buffer.
	if gotEmitted, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_emitted_total"), int64(1+validCount+1); gotEmitted != want {
		t.Fatalf("emitted = %d, want %d", gotEmitted, want)
	}
}

// TestAsyncAppender_TotalSinkOutageCountsEveryEvent proves the per-event
// fallback does not UNDER-count a genuine total outage: when the sink rejects
// every event (not just one poison event), each event fails its individual
// append and every one is counted, so the isolation path never hides a real
// full-store failure.
func TestAsyncAppender_TotalSinkOutageCountsEveryEvent(t *testing.T) {
	t.Parallel()

	sink := &fakeSink{err: fmt.Errorf("store unreachable")}
	metrics, reader := testMetrics(t)
	appender := NewAsyncAppender(sink, metrics, WithBufferCapacity(64))

	const n = 5
	for i := 0; i < n; i++ {
		if err := appender.Append(context.Background(), []governanceaudit.Event{testEvent(fmt.Sprintf("event-%d", i))}); err != nil {
			t.Fatalf("Append() %d error = %v", i, err)
		}
	}
	if err := appender.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got, want := counterValue(t, reader, "eshu_dp_governance_audit_allowed_persist_failures_total"), int64(n); got != want {
		t.Fatalf("persist failures = %d, want %d (a total outage must count every event)", got, want)
	}
}
