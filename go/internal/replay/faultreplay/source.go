// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// claimSource is the seam FaultingWorkSource decorates: the reducer claim
// interfaces already implemented by schedulereplay.ScheduledWorkSource, plus
// Drained so FaultingWorkSource can report its own composed drain state.
// Declaring it locally (rather than depending on a concrete schedulereplay
// type) keeps this package wrapping the seam, matching the precedent
// decorators (cmd/reducer's activeWorkerExecutor, cypher.BackpressureExecutor):
// wrap the interface, do not reach past it.
type claimSource interface {
	reducer.WorkSource
	reducer.BatchWorkSource
	Drained() bool
}

// redeliverer is the callback surface FaultingExecutor uses to trigger the two
// redelivery mechanics FaultingWorkSource owns: a blocking mid-handler
// rendezvous (expire-lease-mid-handler) and a fire-and-forget requeue
// (fail-graph-write-once-then-succeed's queue-retry lane). Keeping this as a
// small interface -- rather than having FaultingExecutor reach into
// FaultingWorkSource's fields -- is what lets the two decorators stay
// independently testable while still cooperating on one shared redelivery
// queue, the same way production RetryingExecutor and BackpressureExecutor
// compose independently over one Postgres queue.
type redeliverer interface {
	// ArmMidHandlerDuplicate enqueues a concurrent duplicate delivery of intent
	// and returns a channel that closes once a Claim/ClaimBatch call has
	// actually handed that duplicate to a (necessarily different) worker. The
	// caller is expected to block on the returned channel before applying its
	// own copy, so there is a real window where two workers are in-flight on
	// the same intent (T4).
	ArmMidHandlerDuplicate(intent reducer.Intent) <-chan struct{}
	// RedeliverOnce enqueues a fire-and-forget duplicate delivery of intent,
	// with no claimed-signal to wait on. It models the queue-retry lane: a
	// plain Execute error surfaced WorkSink.Fail in production would leave the
	// row re-claimable through Postgres; this hermetic tier has no Postgres, so
	// the fault decorator re-arms the redelivery directly.
	RedeliverOnce(intent reducer.Intent)
}

// pendingRedelivery is one intent queued for out-of-band (fault-scripted)
// redelivery. claimedSignal is non-nil only for a mid-handler duplicate --
// closing it is the rendezvous signal the parked handler is waiting for.
type pendingRedelivery struct {
	intent        reducer.Intent
	claimedSignal chan struct{}
}

// FaultingWorkSource decorates a claimSource (schedulereplay's
// ScheduledWorkSource in every current caller) with the two delivery-affecting
// faults from the Layer 4 script vocabulary: kill-worker-after-claim and the
// redelivery mechanics expire-lease-mid-handler and the queue-retry lane of
// fail-graph-write-once-then-succeed need. None of these kill a real
// goroutine, expire a real lease, or touch a real queue row -- consistent with
// script.go's Trigger doc comment, every effect is driven by an ordinal over
// observed claims, never a timer, so a fault run stays byte-replayable.
//
// FaultingWorkSource is safe for concurrent use: the concurrent reducer worker
// pool competes for claims through it exactly as it would compete through the
// production Postgres queue. The only lock is mu, guarding the pending
// redelivery queue and the kill-worker fire-once set; it is held only for
// slice/map bookkeeping, never across a Claim call to inner, so it cannot nest
// with any lock inner or the caller holds.
type FaultingWorkSource struct {
	inner claimSource

	mu      sync.Mutex
	pending []pendingRedelivery
	// killAfterClaims maps a 1-based global claim ordinal to "fire": the claim
	// landing on that ordinal has its intent pushed onto pending. A key is
	// deleted the instant it fires, so (defense in depth, since claimCount is
	// monotonic and every ordinal value is presented at most once) it cannot
	// refire.
	killAfterClaims map[int]struct{}

	claimCount atomic.Int64
	// injected counts every fault-scripted redelivery actually pushed (kill-
	// worker, mid-handler, or queue-retry), so a test can prove a fault fired
	// rather than silently no-op'd.
	injected atomic.Int64
}

// NewFaultingWorkSource wraps inner with the delivery-affecting faults found
// in script. script MUST already be Script.Validate'd (RunFault does this);
// NewFaultingWorkSource trusts the shape and only rejects a combination this
// hermetic tier cannot honor (more than one expire-lease-mid-handler fault, or
// a restart-backend-between-phase-groups fault, which needs a real backend and
// belongs to a later slice) -- fail loudly at construction rather than
// silently ignore a scripted fault that can never fire.
func NewFaultingWorkSource(inner claimSource, script Script) (*FaultingWorkSource, error) {
	s := &FaultingWorkSource{
		inner:           inner,
		killAfterClaims: map[int]struct{}{},
	}
	sawMidHandler := false
	for _, f := range script.Faults {
		switch f.Kind {
		case KindKillWorkerAfterClaim:
			s.killAfterClaims[*f.Trigger.AfterClaims] = struct{}{}
		case KindExpireLeaseMidHandler:
			if sawMidHandler {
				return nil, fmt.Errorf("faulting work source: script names more than one %s fault; only one is supported per run", KindExpireLeaseMidHandler)
			}
			sawMidHandler = true
		case KindRestartBackendBetweenPhaseGroups:
			return nil, fmt.Errorf("faulting work source: %s requires a real backend and is not supported by this hermetic runner", KindRestartBackendBetweenPhaseGroups)
		case KindFailGraphWriteOnceThenSucceed, KindFailTerminal:
			// Executor-seam faults; NewFaultingExecutor owns them.
		default:
			return nil, fmt.Errorf("faulting work source: unknown fault kind %q", f.Kind)
		}
	}
	return s, nil
}

// Claim returns a queued redelivery first (a kill-worker or mid-handler
// duplicate), otherwise the next intent from inner. Every successful claim --
// redelivery or fresh -- increments the global claim ordinal and is checked
// against the script's kill-worker-after-claim triggers.
func (s *FaultingWorkSource) Claim(ctx context.Context) (reducer.Intent, bool, error) {
	if pr, ok := s.popPending(); ok {
		s.recordClaim(pr.intent)
		if pr.claimedSignal != nil {
			close(pr.claimedSignal)
		}
		return pr.intent, true, nil
	}
	intent, ok, err := s.inner.Claim(ctx)
	if err != nil {
		return reducer.Intent{}, false, fmt.Errorf("faultreplay: inner work source claim: %w", err)
	}
	if !ok {
		return reducer.Intent{}, false, nil
	}
	s.recordClaim(intent)
	return intent, true, nil
}

// ClaimBatch fills up to limit intents the same way Claim does: queued
// redeliveries first, then a delegated inner.ClaimBatch for the remainder. At
// most one pending redelivery is folded into a given batch call so its
// claimed-signal semantics (one specific duplicate, one specific waiter) stay
// unambiguous.
func (s *FaultingWorkSource) ClaimBatch(ctx context.Context, limit int) ([]reducer.Intent, error) {
	if limit <= 0 {
		return nil, nil
	}
	var out []reducer.Intent
	if pr, ok := s.popPending(); ok {
		s.recordClaim(pr.intent)
		if pr.claimedSignal != nil {
			close(pr.claimedSignal)
		}
		out = append(out, pr.intent)
		limit--
	}
	if limit <= 0 {
		return out, nil
	}
	batch, err := s.inner.ClaimBatch(ctx, limit)
	if err != nil {
		return out, fmt.Errorf("faultreplay: inner work source claim batch: %w", err)
	}
	for _, intent := range batch {
		s.recordClaim(intent)
		out = append(out, intent)
	}
	return out, nil
}

// Drained reports whether every scripted intent AND every fault-injected
// redelivery has been claimed. Because a redelivery can be enqueued after
// inner has already exhausted its fixed schedule (a kill-worker fault on the
// last scripted claim, or a mid-handler fault on the last-delivered intent),
// Drained alone can flicker true/false/true; RunFault's awaitDrain always ANDs
// it with the sink's processed count reaching the (fault-adjusted) total, so
// that flicker never lets a partial run report green.
func (s *FaultingWorkSource) Drained() bool {
	s.mu.Lock()
	pendingEmpty := len(s.pending) == 0
	s.mu.Unlock()
	return pendingEmpty && s.inner.Drained()
}

// UnfiredFaults reports every kill-worker-after-claim fault that was
// scripted but never fired: its after_claims ordinal was never reached by an
// actual claim before the run drained. RunFault calls this after the run has
// fully drained; a non-empty result means the script is inert for that
// fault -- an after_claims ordinal beyond the number of claims a schedule
// ever generates would otherwise let the fault-free graph snapshot through
// unnoticed.
func (s *FaultingWorkSource) UnfiredFaults() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.killAfterClaims) == 0 {
		return nil
	}
	ordinals := make([]int, 0, len(s.killAfterClaims))
	for ordinal := range s.killAfterClaims {
		ordinals = append(ordinals, ordinal)
	}
	sort.Ints(ordinals)
	out := make([]string, 0, len(ordinals))
	for _, ordinal := range ordinals {
		out = append(out, fmt.Sprintf("%s(after_claims=%d)", KindKillWorkerAfterClaim, ordinal))
	}
	return out
}

// InjectedRedeliveries reports how many fault-scripted redeliveries this
// source has actually pushed (kill-worker, mid-handler, or queue-retry). A
// test asserts this is non-zero for a script that names such a fault, proving
// the fault fired instead of silently no-op'ing.
func (s *FaultingWorkSource) InjectedRedeliveries() int64 {
	return s.injected.Load()
}

// ArmMidHandlerDuplicate implements redeliverer for FaultingExecutor: it
// enqueues a concurrent duplicate of intent and returns the channel that
// closes once some Claim/ClaimBatch call actually hands that duplicate to a
// worker.
func (s *FaultingWorkSource) ArmMidHandlerDuplicate(intent reducer.Intent) <-chan struct{} {
	ch := make(chan struct{})
	s.mu.Lock()
	s.pending = append(s.pending, pendingRedelivery{intent: intent, claimedSignal: ch})
	s.mu.Unlock()
	s.injected.Add(1)
	return ch
}

// RedeliverOnce implements redeliverer for FaultingExecutor: it enqueues a
// fire-and-forget duplicate of intent with no claimed-signal to wait on.
func (s *FaultingWorkSource) RedeliverOnce(intent reducer.Intent) {
	s.mu.Lock()
	s.pending = append(s.pending, pendingRedelivery{intent: intent})
	s.mu.Unlock()
	s.injected.Add(1)
}

// popPending removes and returns the oldest queued redelivery, if any.
func (s *FaultingWorkSource) popPending() (pendingRedelivery, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return pendingRedelivery{}, false
	}
	pr := s.pending[0]
	s.pending = s.pending[1:]
	return pr, true
}

// recordClaim increments the global claim ordinal and, if that ordinal is a
// kill-worker-after-claim trigger, enqueues the just-claimed intent for later
// redelivery (modeling the worker-death + lease-reclaim class: the intent is
// re-delivered without the original claim's handler ever actually being
// killed, since Go has no safe mechanism to kill an arbitrary goroutine mid-
// flight -- the observable effect, a duplicate delivery, is what the fault
// exercises).
func (s *FaultingWorkSource) recordClaim(intent reducer.Intent) {
	ordinal := int(s.claimCount.Add(1))
	s.mu.Lock()
	_, fire := s.killAfterClaims[ordinal]
	if fire {
		delete(s.killAfterClaims, ordinal)
		s.pending = append(s.pending, pendingRedelivery{intent: intent})
	}
	s.mu.Unlock()
	if fire {
		s.injected.Add(1)
	}
}
