// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifafaultinjection

package cypher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay/faultreplay"
)

// ifaFaultSentinelPollInterval is how often the restart-backend-between-
// phase-groups fault polls for the sentinel file's removal while blocked.
// This is a poll, not a wall-clock trigger for the fault itself: the fault
// FIRES on an observed phase-group ordinal (deterministic); only the wait
// for the harness's external restart-and-release action is time-based, and
// that wait is bounded by ctx, never open-ended (see maybeRestartAfterGroup).
const ifaFaultSentinelPollInterval = 200 * time.Millisecond

var (
	// errFaultingExecutorInnerNoGroup is returned by ExecuteGroup when the
	// wrapped executor does not implement GroupExecutor. Fails closed rather
	// than silently no-op'ing so a caller notices the capability mismatch
	// immediately instead of losing a batch of writes.
	errFaultingExecutorInnerNoGroup = errors.New("faulting executor: inner executor does not support ExecuteGroup")
	// errFaultingExecutorInnerNoPhaseGroup is the ExecutePhaseGroup
	// counterpart of errFaultingExecutorInnerNoGroup.
	errFaultingExecutorInnerNoPhaseGroup = errors.New("faulting executor: inner executor does not support ExecutePhaseGroup")
)

// FaultingExecutor decorates a cypher Executor with the graph-executor-seam
// faults from the Layer 4 fault-script vocabulary
// (go/internal/replay/faultreplay): fail-graph-write-once-then-succeed (both
// lanes) and restart-backend-between-phase-groups. It exists ONLY under the
// ifafaultinjection build tag (see fault_executor_off.go); no production,
// CI, or default-tag build ever links this decorator or the fault-script
// reader that constructs it (go/cmd/reducer's ifa_fault_wiring.go, same
// tag). This is issue #4580 Layer 4 / P6 slice S4 -- the in-binary decorator
// the (separate, deferred) Docker gate verify-ifa-fault-injection.sh will
// use to inject faults into the real eshu-reducer binary; building and unit
// testing this decorator needs no Docker at all.
//
// # Placement and lane honesty (T1, proven before this slice)
//
// This decorator wraps the reducer's BASE cypher.Executor: the exact value
// go/cmd/reducer's executeReducerCypherWithRetry re-wraps, on every single
// call, in a brand-new sourcecypher.RetryingExecutor around the real
// session runner. That RetryingExecutor lives INSIDE the base executor's
// Execute method -- below this decorator in the call chain, not above it.
// Consequently, for fail-graph-write-once-then-succeed:
//
//   - target.lane = queue-retry returns a retryable, self-classifying
//     graph_write_timeout error (ifaFaultQueueRetryError, matching a real
//     *neo4jRetryableError) without ever calling the inner executor for the
//     targeted call, so the failure genuinely surfaces to reducer.Service's
//     WorkSink.Fail and is re-enqueued as 'retrying': the queue-retry path
//     this lane claims to exercise, proven for real, recovering on the next
//     claim exactly as a real fail-once transient does.
//   - target.lane = executor-retry shapes its injected error to contain
//     "TransientError" (satisfying isTransientNeo4jError, and therefore
//     isRetryableGraphWriteError/isRetryableGraphWriteGroupError). But
//     because this decorator sits ABOVE the per-call RetryingExecutor
//     rather than below it, no RetryingExecutor ever observes this error:
//     it short-circuits before inner.Execute is called at all, so it
//     surfaces to WorkSink.Fail and recovers via queue-retry exactly like
//     the queue-retry lane (it carries the same retryable graph_write_timeout
//     contract). This is the honest limitation T1 established: reaching the
//     real in-place executor-retry loop in the reducer binary would require
//     restructuring executeReducerCypherWithRetry to accept an injectable
//     inner runner, tracked in #5048 and out of scope for this slice. See
//     TestFaultingExecutorExecutorRetryLaneIsShapedTransientButNeverRetriedAboveTheSeam
//     for the regression proof (wrapping a real RetryingExecutor as inner
//     and asserting its own inner is never called for the fired attempt).
//
// # Capability passthrough
//
// Execute, ExecuteGroup, and ExecutePhaseGroup are all defined unconditionally
// on *FaultingExecutor (mirroring BackpressureExecutor's ExecuteGroup, which
// also always exists and fails closed via errInnerNoExecuteGroup when inner
// does not support it, rather than a second execute-only wrapper type).
// go/cmd/reducer's canonical writer (canonical_node_writer.go) type-asserts
// GroupExecutor BEFORE PhaseGroupExecutor and takes the first match, so for
// the wiring this slice adds -- where inner always supports GroupExecutor --
// the unconditionally-present ExecutePhaseGroup method is never reached; a
// future caller wrapping a PhaseGroupExecutor-only inner would still fail
// closed via errFaultingExecutorInnerNoPhaseGroup rather than silently
// dropping the phase-group fault.
type FaultingExecutor struct {
	inner Executor

	// fail-graph-write-once-then-succeed state.
	onceOrdinal int    // 0 => match by substring instead of ordinal.
	onceMatch   string // "" => match by ordinal instead of substring.
	onceLane    string // "" => no such fault scripted.
	onceFired   atomic.Bool

	// restart-backend-between-phase-groups state.
	restartAfterGroups int // 0 => no such fault scripted.
	restartFired       atomic.Bool
	sentinelPath       string

	callOrdinal  atomic.Int64 // shared across Execute/ExecuteGroup/ExecutePhaseGroup.
	groupOrdinal atomic.Int64 // advances once per completed ExecuteGroup/ExecutePhaseGroup.
}

// NewFaultingExecutor wraps inner with the fail-graph-write-once-then-succeed
// and restart-backend-between-phase-groups faults found in script. script
// MUST already be Script.Validate'd (faultreplay.Load does this). The other
// three fault kinds (kill-worker-after-claim, expire-lease-mid-handler,
// fail-terminal) target the WorkSource/intent seam, not this graph-executor
// seam; a script naming them alongside a graph-executor fault is accepted
// (one script may combine faults across seams for a single fault run) but
// those kinds are inert here by design -- a different decorator owns them.
//
// sentinelPath is the file path the restart-backend-between-phase-groups
// fault creates when it pauses and polls for removal. It MUST be non-empty
// when the script contains that fault kind; construction fails closed
// otherwise rather than silently building an unusable fault.
func NewFaultingExecutor(inner Executor, script faultreplay.Script, sentinelPath string) (Executor, error) {
	fe := &FaultingExecutor{inner: inner}
	for _, f := range script.Faults {
		if err := fe.applyFault(f, sentinelPath); err != nil {
			return nil, err
		}
	}
	return fe, nil
}

// applyFault records one scripted fault op's effect on fe, or returns a
// construction-time error for an ambiguous or unusable script.
func (fe *FaultingExecutor) applyFault(f faultreplay.FaultOp, sentinelPath string) error {
	switch f.Kind {
	case faultreplay.KindFailGraphWriteOnceThenSucceed:
		if fe.onceLane != "" {
			return fmt.Errorf("faulting executor: script names more than one %s fault; only one is supported per run", faultreplay.KindFailGraphWriteOnceThenSucceed)
		}
		fe.onceLane = f.Target.Lane
		if f.Trigger.OperationMatch != nil {
			fe.onceMatch = *f.Trigger.OperationMatch
		} else {
			fe.onceOrdinal = *f.Trigger.StatementOrdinal
		}
		return nil
	case faultreplay.KindRestartBackendBetweenPhaseGroups:
		if fe.restartAfterGroups != 0 {
			return fmt.Errorf("faulting executor: script names more than one %s fault; only one is supported per run", faultreplay.KindRestartBackendBetweenPhaseGroups)
		}
		if strings.TrimSpace(sentinelPath) == "" {
			return fmt.Errorf("faulting executor: %s requires a non-empty sentinel path", faultreplay.KindRestartBackendBetweenPhaseGroups)
		}
		fe.restartAfterGroups = *f.Trigger.AfterPhaseGroups
		fe.sentinelPath = sentinelPath
		return nil
	case faultreplay.KindKillWorkerAfterClaim, faultreplay.KindExpireLeaseMidHandler, faultreplay.KindFailTerminal:
		// WorkSource/intent-seam faults; a different decorator owns them,
		// not this graph-executor seam. Deliberately inert here.
		return nil
	default:
		return fmt.Errorf("faulting executor: unknown fault kind %q", f.Kind)
	}
}

// OnceThenSucceedFired reports whether the scripted fail-graph-write-once-
// then-succeed fault (if any) has already fired. Tests and the (deferred)
// gate script use this to prove the fault genuinely ran rather than silently
// no-op'ing -- the P3 "measured inert" lesson applied to this seam.
func (fe *FaultingExecutor) OnceThenSucceedFired() bool {
	return fe.onceLane != "" && fe.onceFired.Load()
}

// RestartFired reports whether the scripted restart-backend-between-phase-
// groups fault (if any) has already fired.
func (fe *FaultingExecutor) RestartFired() bool {
	return fe.restartAfterGroups != 0 && fe.restartFired.Load()
}

// Execute applies the scripted fail-graph-write-once-then-succeed fault (by
// shared call ordinal or operation-match against stmt.Cypher) before
// delegating to inner. See the type doc for which lane this actually reaches.
func (fe *FaultingExecutor) Execute(ctx context.Context, stmt Statement) error {
	ordinal := int(fe.callOrdinal.Add(1))
	if err := fe.maybeFailOnce(ordinal, []Statement{stmt}); err != nil {
		return err
	}
	return fe.inner.Execute(ctx, stmt)
}

// ExecuteGroup applies the same fail-graph-write-once-then-succeed fault
// (matching any statement in stmts) before delegating to inner, then, once
// the group has committed, advances the phase-group ordinal and applies the
// restart-backend-between-phase-groups fault. Returns
// errFaultingExecutorInnerNoGroup if inner does not support grouped writes.
func (fe *FaultingExecutor) ExecuteGroup(ctx context.Context, stmts []Statement) error {
	ge, ok := fe.inner.(GroupExecutor)
	if !ok {
		return errFaultingExecutorInnerNoGroup
	}
	ordinal := int(fe.callOrdinal.Add(1))
	if err := fe.maybeFailOnce(ordinal, stmts); err != nil {
		return err
	}
	if err := ge.ExecuteGroup(ctx, stmts); err != nil {
		return err
	}
	return fe.maybeRestartAfterGroup(ctx, int(fe.groupOrdinal.Add(1)))
}

// ExecutePhaseGroup mirrors ExecuteGroup for the narrower PhaseGroupExecutor
// surface (bootstrap-index and ingester's bounded per-phase writers). Returns
// errFaultingExecutorInnerNoPhaseGroup if inner does not support it.
func (fe *FaultingExecutor) ExecutePhaseGroup(ctx context.Context, stmts []Statement) error {
	pge, ok := fe.inner.(PhaseGroupExecutor)
	if !ok {
		return errFaultingExecutorInnerNoPhaseGroup
	}
	ordinal := int(fe.callOrdinal.Add(1))
	if err := fe.maybeFailOnce(ordinal, stmts); err != nil {
		return err
	}
	if err := pge.ExecutePhaseGroup(ctx, stmts); err != nil {
		return err
	}
	return fe.maybeRestartAfterGroup(ctx, int(fe.groupOrdinal.Add(1)))
}

// maybeFailOnce fires the scripted fail-graph-write-once-then-succeed fault
// the first time ordinal (or any statement in stmts, for an operation-match
// trigger) matches, returning a lane-shaped error instead of delegating. A
// sync/atomic CompareAndSwap gate makes this safe under concurrent callers:
// exactly one caller observes the fired error, even if many match.
func (fe *FaultingExecutor) maybeFailOnce(ordinal int, stmts []Statement) error {
	if fe.onceLane == "" || !fe.onceMatches(ordinal, stmts) {
		return nil
	}
	if !fe.onceFired.CompareAndSwap(false, true) {
		return nil
	}
	switch fe.onceLane {
	case faultreplay.LaneQueueRetry:
		return &ifaFaultQueueRetryError{ordinal: ordinal}
	case faultreplay.LaneExecutorRetry:
		return &ifaFaultExecutorRetryShapedError{ordinal: ordinal}
	default:
		return fmt.Errorf("faulting executor: unknown target.lane %q", fe.onceLane)
	}
}

// onceMatches reports whether the current call is the one
// fail-graph-write-once-then-succeed targets: either the callOrdinal-th
// graph-write call (Execute or ExecuteGroup, sharing one ordinal sequence),
// or a call carrying at least one statement whose Cypher text contains the
// scripted substring.
func (fe *FaultingExecutor) onceMatches(ordinal int, stmts []Statement) bool {
	if fe.onceMatch != "" {
		for i := range stmts {
			if strings.Contains(stmts[i].Cypher, fe.onceMatch) {
				return true
			}
		}
		return false
	}
	return ordinal == fe.onceOrdinal
}

// maybeRestartAfterGroup fires the scripted restart-backend-between-phase-
// groups fault the first time groupOrdinal reaches the scripted threshold,
// AFTER the just-completed phase group's write has already landed against
// inner. It writes a sentinel file, then blocks -- polling for the
// sentinel's removal -- until the file disappears or ctx is done. The
// (deferred, out of this slice's scope) Docker gate script is expected to
// restart the graph backend while this call is blocked, then delete the
// sentinel file to release it. ctx.Done() always wins over an unremoved
// sentinel, so this can never deadlock a caller that supplies a bounded
// context.
func (fe *FaultingExecutor) maybeRestartAfterGroup(ctx context.Context, groupOrdinal int) error {
	if fe.restartAfterGroups == 0 || groupOrdinal != fe.restartAfterGroups {
		return nil
	}
	if !fe.restartFired.CompareAndSwap(false, true) {
		return nil
	}
	// #nosec G306 -- sentinel is a local/CI fault-injection coordination flag
	// file for the (deferred) Docker gate script, not user or request data;
	// 0o644 lets the operator/gate script read and remove it.
	if err := os.WriteFile(fe.sentinelPath, []byte("waiting-for-backend-restart\n"), 0o644); err != nil {
		return fmt.Errorf("ifa fault: %s: write sentinel %q: %w", faultreplay.KindRestartBackendBetweenPhaseGroups, fe.sentinelPath, err)
	}
	ticker := time.NewTicker(ifaFaultSentinelPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("ifa fault: %s: canceled waiting for sentinel %q removal: %w", faultreplay.KindRestartBackendBetweenPhaseGroups, fe.sentinelPath, ctx.Err())
		case <-ticker.C:
			if _, err := os.Stat(fe.sentinelPath); os.IsNotExist(err) {
				return nil
			}
		}
	}
}

// ifaFaultQueueRetryError is returned once for the queue-retry lane of a
// fail-graph-write-once-then-succeed fault. It carries the same contract a
// real exhausted-transient graph write surfaces to reducer.Service's
// WorkSink.Fail: Retryable() true and FailureClass() graph_write_timeout,
// matching *neo4jRetryableError (the shape WrapRetryableNeo4jError produces for
// a driver TransactionExecutionLimit or ConnectivityError). That makes
// WorkSink.Fail re-enqueue the intent as 'retrying' -- the queue-retry path
// this lane names -- so the once-fault is consumed on the first attempt and the
// intent succeeds on the next claim, exactly as a real fail-once transient
// recovers. A plain error without this contract would instead be non-retryable:
// reducer.IsRetryable would be false, the intent would dead-letter at attempt 1,
// and the dead-letter triage default (projector.ClassifyFailure) would mislabel
// a reducer graph write as projection_bug. The fault must model the real
// transient, not an opaque error no real transient resembles.
type ifaFaultQueueRetryError struct{ ordinal int }

func (e *ifaFaultQueueRetryError) Error() string {
	return fmt.Sprintf("ifa fault: %s (queue-retry) injected one failure for graph-write call #%d",
		faultreplay.KindFailGraphWriteOnceThenSucceed, e.ordinal)
}

// Retryable reports the fault error as retryable so reducer.IsRetryable (an
// errors.As probe for exactly this method) routes it to WorkSink.Fail's
// queue-retry branch instead of a dead letter, as a real transient graph write
// would be routed.
func (*ifaFaultQueueRetryError) Retryable() bool { return true }

// FailureClass records the graph-write-timeout class a real exhausted-transient
// graph write carries (see *neo4jRetryableError.FailureClass), so the retrying
// row is labeled honestly rather than defaulting to projection_bug.
func (*ifaFaultQueueRetryError) FailureClass() string { return GraphWriteTimeoutFailureClass }

// ifaFaultExecutorRetryShapedError is returned once for the executor-retry
// lane. Its message contains "TransientError" so isTransientNeo4jError (and
// therefore isRetryableGraphWriteError/isRetryableGraphWriteGroupError) would
// classify it as retryable IF a RetryingExecutor sat below this decorator in
// the call chain. In go/cmd/reducer's actual wiring it does not: this decorator
// wraps the same base executor executeReducerCypherWithRetry re-wraps in a
// fresh RetryingExecutor per call, so it sits above that RetryingExecutor and
// the error surfaces to WorkSink.Fail exactly like the queue-retry lane rather
// than being retried in place. It therefore carries the SAME retryable
// graph_write_timeout contract as the queue-retry lane (via Retryable() and
// FailureClass() below) so the intent recovers via queue-retry with zero dead
// letters instead of dead-lettering as projection_bug. The message still names
// the executor-retry lane and a TransientError shape so a genuine below-the-seam
// RetryingExecutor -- the hermetic tier's FaultingExecutor -- classifies it
// transient and retries it in place. Reaching the real in-place retry loop in
// the reducer binary needs executeReducerCypherWithRetry restructured to accept
// an injectable inner runner; that is tracked in #5048 and out of this slice's
// scope.
type ifaFaultExecutorRetryShapedError struct{ ordinal int }

func (e *ifaFaultExecutorRetryShapedError) Error() string {
	return fmt.Sprintf(
		"ifa fault: %s (executor-retry, Neo.TransientError.Transaction.LockClientStopped-shaped) injected one failure for graph-write call #%d",
		faultreplay.KindFailGraphWriteOnceThenSucceed, e.ordinal)
}

// Retryable reports the fault error as retryable so that, at the wrap point
// above the reducer's per-call RetryingExecutor, WorkSink.Fail queue-retries it
// (issue #5048 tracks moving the decorator below RetryingExecutor for true
// in-place retry).
func (*ifaFaultExecutorRetryShapedError) Retryable() bool { return true }

// FailureClass records the graph-write-timeout class so the retrying row is
// labeled identically to the queue-retry lane and to a real transient graph
// write.
func (*ifaFaultExecutorRetryShapedError) FailureClass() string {
	return GraphWriteTimeoutFailureClass
}
