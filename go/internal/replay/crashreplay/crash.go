// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crashreplay

import (
	"context"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// CrashKind selects where in the claim/execute/ack lifecycle the scripted crash
// fires.
type CrashKind int

const (
	// CrashBeforeClaim fires at a clean boundary: after N items are durably
	// completed and before the next is claimed. No lease is held across the
	// crash — it proves recovery never redoes already-completed work.
	CrashBeforeClaim CrashKind = iota
	// CrashAfterApply fires inside the dirty post-lease-pre-complete window: the
	// crash item is claimed and projected to the graph, then the worker dies
	// before the completion ack. The item is left holding a durable lease, so
	// recovery must reclaim it (after the lease lapses) and idempotently
	// re-project it.
	CrashAfterApply
)

// String renders the crash kind for error and report messages.
func (k CrashKind) String() string {
	switch k {
	case CrashBeforeClaim:
		return "before-claim"
	case CrashAfterApply:
		return "after-apply"
	default:
		return fmt.Sprintf("crash-kind(%d)", int(k))
	}
}

// CrashPoint scripts one crash: its kind and how many work items complete before
// it fires. After=2 means two items are acked before the crash. In the
// single-worker model this package runs, an item is applied and then immediately
// acked, so the applied count (which drives CrashAfterApply) equals the acked
// count (which drives CrashBeforeClaim); After is therefore the
// durably-completed count for both kinds.
type CrashPoint struct {
	Kind  CrashKind
	After int
}

// validate rejects an obviously malformed crash point. An unreachable After
// (one that never fires before the schedule drains) is not caught here — it is
// caught at run time and reported as "crash never triggered", so a misconfigured
// scenario fails loudly instead of silently running a no-crash pass.
func (c CrashPoint) validate() error {
	if c.After < 0 {
		return fmt.Errorf("crashreplay: crash point After must be >= 0, got %d", c.After)
	}
	return nil
}

// crashSignal is the type of the crash sentinel; a dedicated unexported type
// keeps the panic value unambiguous.
type crashSignal struct{}

// crashSentinel is the panic value the crash decorators raise to simulate a
// process death. The run goroutine recovers exactly this value (by pointer
// identity) and treats it as a crash; any other panic is re-raised so real bugs
// are not swallowed.
var crashSentinel = &crashSignal{}

// fatalSignal is the type of the fatal sentinel.
type fatalSignal struct{}

// fatalSentinel is the panic value the executor raises on an unrecoverable error
// (an unknown intent, or a context cancellation mid-execute). It exists because
// the reducer loop turns an Execute error into a WorkSink.Fail + continue, which
// against this store re-queues the item and spins forever instead of stopping.
// Panicking unwinds the loop so the run goroutine recovers it and surfaces the
// recorded error (firstErr) immediately, rather than hanging to the CI timeout.
var fatalSentinel = &fatalSignal{}

// crashController is a one-shot trigger shared by the crashing source and
// executor. Exactly one of them fires, decided by Kind; once fired it disarms so
// the recovery phase (which reuses neither decorator) can never re-crash.
type crashController struct {
	mu    sync.Mutex
	kind  CrashKind
	after int
	fired bool
}

// tryFire reports whether the crash should fire now: the controller is armed,
// the event kind matches, and count (durably-completed items so far) equals the
// scripted After. It is edge-triggered and one-shot — the first match sets fired
// and every later call returns false.
func (c *crashController) tryFire(kind CrashKind, count int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fired || c.kind != kind || count != c.after {
		return false
	}
	c.fired = true
	return true
}

// crashingSource wraps the durable store and injects a CrashBeforeClaim crash.
// It counts claims that returned an item; on the claim that would deliver item
// number After (zero-based), it panics before delegating, so that item is never
// claimed and the prior items are already completed — a clean crash boundary.
type crashingSource struct {
	inner *durableStore
	crash *crashController
}

// Claim panics with crashSentinel at the scripted clean boundary; otherwise it
// delegates to the durable store. The completed-item count drives the trigger,
// read from the store so it reflects durable acks rather than claim attempts.
func (s *crashingSource) Claim(ctx context.Context) (reducer.Intent, bool, error) {
	if s.crash.tryFire(CrashBeforeClaim, s.inner.ackedCount()) {
		panic(crashSentinel)
	}
	return s.inner.Claim(ctx)
}

// graphExecutor projects each claimed intent's work item into the shared graph
// and injects a CrashAfterApply crash. The graph is the durable projection
// target: it survives the crash with the harness, mirroring committed graph
// writes surviving a process death. Mutations are serialized behind a mutex so
// the executor is safe even though crash runs are single-worker.
type graphExecutor struct {
	registry map[string]schedulereplay.WorkItem
	graph    *schedulereplay.Graph
	apply    schedulereplay.Applier
	crash    *crashController

	mu      sync.Mutex
	applies int
	err     error
}

// Execute applies the claimed item to the graph. On the scripted CrashAfterApply
// item it applies the work (the durable graph write) and then panics before
// returning, so the reducer loop never acks it — leaving the dirty
// post-lease-pre-complete state recovery must repair. The crash decision uses
// the count of items applied so far, so After completed applies precede the
// crash item.
func (e *graphExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		// Record then panic the fatal sentinel rather than returning an error:
		// a returned error makes the reducer loop Fail + re-queue + spin. The
		// deferred Unlock runs as the panic unwinds, so the mutex is released.
		e.recordErrLocked(fmt.Errorf("crashreplay execute canceled: %w", err))
		panic(fatalSentinel)
	}
	item, ok := e.registry[intent.IntentID]
	if !ok {
		e.recordErrLocked(fmt.Errorf("crashreplay: no work item for intent %q", intent.IntentID))
		panic(fatalSentinel)
	}
	if e.crash != nil && e.crash.tryFire(CrashAfterApply, e.applies) {
		// Apply the durable graph write, then die before the ack. The deferred
		// Unlock runs as the panic unwinds, so the mutex is released cleanly and
		// the recovery executor (a fresh instance) is never blocked.
		e.apply(e.graph, item)
		panic(crashSentinel)
	}
	e.apply(e.graph, item)
	e.applies++
	return reducer.Result{
		IntentID: intent.IntentID,
		Domain:   intent.Domain,
		Status:   reducer.ResultStatusSucceeded,
	}, nil
}

// recordErrLocked stores the first execute error. The caller must hold e.mu and
// then panic(fatalSentinel); the run goroutine recovers it and surfaces this
// stored error via firstErr, the loud-failure backstop the runner checks before
// snapshotting.
func (e *graphExecutor) recordErrLocked(err error) {
	if e.err == nil {
		e.err = err
	}
}

// firstErr returns the first execute error recorded, or nil.
func (e *graphExecutor) firstErr() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.err
}
