// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

// onceThenSucceedFault is at most one fail-graph-write-once-then-succeed
// fault. This hermetic tier has exactly one graph-write call per Execute (the
// harness has no separate per-statement Cypher boundary), so StatementOrdinal
// counts Execute calls and OperationMatch matches against the intent's
// IntentID -- the only "operation" text this tier has to match against.
type onceThenSucceedFault struct {
	ordinal int    // 0 means match by substring instead (see match)
	match   string // "" means match by ordinal instead
	lane    string

	fired atomic.Bool
}

// matches reports whether the callOrdinal-th Execute call, for intent, is the
// one this fault targets.
func (f *onceThenSucceedFault) matches(callOrdinal int, intent reducer.Intent) bool {
	if f == nil {
		return false
	}
	if f.match != "" {
		return strings.Contains(intent.IntentID, f.match)
	}
	return callOrdinal == f.ordinal
}

// terminalFailure is returned, on every delivery, for an intent a fail-
// terminal fault targets. It is a plain error (no Retryable method), so
// reducer.IsRetryable(err) is false and nothing in this hermetic tier ever
// re-arms a redelivery for it -- the intent stays in the sink's failed set,
// which is exactly the durable-failure behavior fail-terminal exists to prove.
type terminalFailure struct{ intentID string }

func (e *terminalFailure) Error() string {
	return fmt.Sprintf("faultreplay: fail-terminal intent %q injected a durable failure", e.intentID)
}

// queueRetryFailure is returned once for the queue-retry lane of a fail-
// graph-write-once-then-succeed fault. Like terminalFailure it is a plain
// error (not RetryableError-marked): production's queue-retry lane is exactly
// a plain error surfacing to WorkSink.Fail, as opposed to the executor-retry
// lane's transient-classified error retried in place. The redelivery that
// makes the retried attempt actually happen is armed by the executor calling
// redeliverer.RedeliverOnce before returning this error, not by anything
// inspecting the error itself.
type queueRetryFailure struct{ intentID string }

func (e *queueRetryFailure) Error() string {
	return fmt.Sprintf("faultreplay: fail-graph-write-once-then-succeed (queue-retry) injected one failure for intent %q", e.intentID)
}

// FaultingExecutor decorates a reducer.Executor with the graph-write faults
// from the Layer 4 script vocabulary: fail-graph-write-once-then-succeed
// (both lanes) and fail-terminal. It mirrors cmd/reducer's
// activeWorkerExecutor decorator shape (a private struct wrapping Executor,
// one Execute method) and, for the executor-retry lane, never lets the
// injected failure leave Execute -- matching
// internal/storage/cypher.RetryingExecutor's retry-inside-Execute precedent --
// so the reducer.Service loop observes exactly one successful call.
//
// FaultingExecutor is safe for concurrent use. Its only mutable state is a set
// of atomics (fire-once gates, the call-ordinal counter); the fault target
// sets built at construction (terminal, midHandlerIntentID, onceThenSucceed's
// ordinal/match/lane) are never mutated after NewFaultingExecutor returns, so
// concurrent reads of them need no lock.
type FaultingExecutor struct {
	inner       reducer.Executor
	redeliverer redeliverer

	// midHandlerIntentID is the resolved (ordinal-or-ID, always resolved to an
	// ID at construction so Execute only ever compares strings) target of an
	// expire-lease-mid-handler fault. Empty means no such fault is scripted.
	midHandlerIntentID string
	midHandlerFired    atomic.Bool

	onceThenSucceed *onceThenSucceedFault

	// terminal names every intent ID a fail-terminal fault targets. Built once
	// at construction; read-only for the run's lifetime.
	terminal map[string]struct{}

	callOrdinal atomic.Int64
}

// NewFaultingExecutor wraps inner with the graph-write faults found in
// script. items is the same scripted delivery order passed to RunFault's
// Config.Items; it resolves an expire-lease-mid-handler fault's IntentOrdinal
// trigger to a concrete intent ID up front, so Execute only ever needs a
// string comparison. script MUST already be Script.Validate'd (RunFault does
// this).
func NewFaultingExecutor(inner reducer.Executor, script Script, items []schedulereplay.WorkItem, redeliv redeliverer) (*FaultingExecutor, error) {
	e := &FaultingExecutor{
		inner:       inner,
		redeliverer: redeliv,
		terminal:    map[string]struct{}{},
	}
	for _, f := range script.Faults {
		switch f.Kind {
		case KindExpireLeaseMidHandler:
			if e.midHandlerIntentID != "" {
				return nil, fmt.Errorf("faulting executor: script names more than one %s fault; only one is supported per run", KindExpireLeaseMidHandler)
			}
			id, err := resolveIntentID(f.Trigger, items)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", KindExpireLeaseMidHandler, err)
			}
			e.midHandlerIntentID = id
		case KindFailGraphWriteOnceThenSucceed:
			if e.onceThenSucceed != nil {
				return nil, fmt.Errorf("faulting executor: script names more than one %s fault; only one is supported per run", KindFailGraphWriteOnceThenSucceed)
			}
			ots := &onceThenSucceedFault{lane: f.Target.Lane}
			if f.Trigger.OperationMatch != nil {
				ots.match = *f.Trigger.OperationMatch
			} else {
				ots.ordinal = *f.Trigger.StatementOrdinal
			}
			e.onceThenSucceed = ots
		case KindFailTerminal:
			e.terminal[*f.Trigger.IntentID] = struct{}{}
		case KindKillWorkerAfterClaim:
			// WorkSource-seam fault; NewFaultingWorkSource owns it.
		case KindRestartBackendBetweenPhaseGroups:
			return nil, fmt.Errorf("faulting executor: %s requires a real backend and is not supported by this hermetic runner", KindRestartBackendBetweenPhaseGroups)
		default:
			return nil, fmt.Errorf("faulting executor: unknown fault kind %q", f.Kind)
		}
	}
	return e, nil
}

// resolveIntentID resolves an expire-lease-mid-handler trigger (IntentID xor
// IntentOrdinal, enforced by script.go's Validate) to a concrete intent ID
// against the scripted delivery order in items.
func resolveIntentID(t Trigger, items []schedulereplay.WorkItem) (string, error) {
	if t.IntentID != nil {
		return *t.IntentID, nil
	}
	ordinal := *t.IntentOrdinal
	if ordinal < 1 || ordinal > len(items) {
		return "", fmt.Errorf("intent_ordinal %d out of range for %d scripted items", ordinal, len(items))
	}
	return items[ordinal-1].IntentID, nil
}

// Execute applies the scripted graph-write faults, in this fixed precedence,
// before delegating to inner:
//
//  1. fail-terminal: if intent is a targeted terminal-failure intent, fail
//     every time, never calling inner. This must win over the other two
//     faults because a durably-failing intent must never accidentally
//     recover through an unrelated redelivery/retry path.
//  2. expire-lease-mid-handler: on the one Execute call for the targeted
//     intent, arm a concurrent duplicate and block until it has been claimed
//     by a (necessarily different, since this goroutine cannot itself call
//     Claim while parked here) worker, then fall through to inner -- so both
//     the original and the duplicate are genuinely in-flight at once (T4).
//  3. fail-graph-write-once-then-succeed: on the callOrdinal-th (or
//     OperationMatch-matching) Execute call, fire exactly once per its lane
//     (see onceThenSucceedFault), then fall through to inner.
func (e *FaultingExecutor) Execute(ctx context.Context, intent reducer.Intent) (reducer.Result, error) {
	callOrdinal := int(e.callOrdinal.Add(1))

	if _, terminal := e.terminal[intent.IntentID]; terminal {
		return reducer.Result{}, &terminalFailure{intentID: intent.IntentID}
	}

	if e.midHandlerIntentID != "" && intent.IntentID == e.midHandlerIntentID && e.midHandlerFired.CompareAndSwap(false, true) {
		released := e.redeliverer.ArmMidHandlerDuplicate(intent)
		select {
		case <-released:
		case <-ctx.Done():
			return reducer.Result{}, fmt.Errorf("faultreplay: %s rendezvous canceled for intent %q: %w",
				KindExpireLeaseMidHandler, intent.IntentID, ctx.Err())
		}
	}

	if f := e.onceThenSucceed; f.matches(callOrdinal, intent) && f.fired.CompareAndSwap(false, true) {
		switch f.lane {
		case LaneQueueRetry:
			e.redeliverer.RedeliverOnce(intent)
			return reducer.Result{}, &queueRetryFailure{intentID: intent.IntentID}
		case LaneExecutorRetry:
			// Retried in place: the transient failure is simulated and
			// absorbed right here, never returned to the caller, so the
			// reducer.Service loop observes exactly one (successful) call for
			// this intent -- matching the RetryingExecutor precedent instead
			// of routing through WorkSink.Fail.
		default:
			return reducer.Result{}, fmt.Errorf("faultreplay: unknown target.lane %q", f.lane)
		}
	}

	result, err := e.inner.Execute(ctx, intent)
	if err != nil {
		return reducer.Result{}, fmt.Errorf("faultreplay: inner executor: %w", err)
	}
	return result, nil
}

// ExecutorRetryFired reports whether this executor's executor-retry-lane
// fail-graph-write-once-then-succeed fault (if any) has fired. A test uses
// this to prove the fault actually ran rather than silently no-op'ing.
func (e *FaultingExecutor) ExecutorRetryFired() bool {
	return e.onceThenSucceed != nil && e.onceThenSucceed.lane == LaneExecutorRetry && e.onceThenSucceed.fired.Load()
}
