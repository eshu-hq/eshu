// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// admissionVerdict is one admission decision. An empty reason admits. readAt
// is the start of the oldest depth read the decision used and seq the newest
// read sequence; together they let the shared holder advance its backoff once
// per read and sleep a waiter until that depth leaves the window (#11233).
// retryAfter is the source latch's RetryAfter on a source_behind deferral
// (#10882 F4), zero otherwise.
type admissionVerdict struct {
	reason       string
	failureClass string
	depth        int64
	retryAfter   time.Duration
	readAt       time.Time
	seq          uint64
	used         bool
}

// admissionDecision returns the verdict for an unattributed batch: the
// deferral reason (empty admits), the failure class that drove a
// graph-write-pressure deferral (empty otherwise), the depth that triggered
// it, and the age and read sequence of the depths it used. The
// graph-write-pressure gate is checked first because it is the leading
// indicator that the backend is timing out; the total-depth gate is the
// trailing safeguard.
//
// The pressure gate counts only retrying reducer rows whose failure_class is
// graph_write_timeout, read from a failure-class-scoped query rather than the
// stage/status-only QueueDepths bucket. This is the #3560 fix: a backlog of
// readiness-not-ready retrying rows (secrets_iam_endpoint_not_ready and other
// *_n classes) reports zero graph-write-timeout depth and therefore never
// false-throttles unrelated reducer admission.
func (w reducerAdmissionWriter) admissionDecision(ctx context.Context) (admissionVerdict, error) {
	return w.admissionDecisionFor(ctx, "")
}

// admissionDecisionFor is admissionDecision plus the per-source leg: when
// sourceSystem names a source latched slowed, the group defers under
// reason source_behind and the verdict carries the latch's RetryAfter (the
// grant delay the poll wait honors, #10882 F4). The source leg is consulted
// after the global database-protection gates so a saturated backend still
// reports its own reason first. An empty source (unattributed intents) skips
// the leg. Both global signals go through the shared holder (#11233); the
// source latch keeps its own cached consult.
func (w reducerAdmissionWriter) admissionDecisionFor(ctx context.Context, sourceSystem string) (admissionVerdict, error) {
	var verdict admissionVerdict
	if w.config.graphWritePressureEnabled() && w.deferral != nil {
		graphWriteTimeoutDepth, err := w.graphWriteTimeoutDepth(ctx)
		if err != nil {
			return admissionVerdict{}, fmt.Errorf("read reducer graph-write-timeout depth: %w", err)
		}
		verdict.observe(graphWriteTimeoutDepth)
		if w.deferral.graphWritePressure(graphWriteTimeoutDepth.value, w.config.RetryingHighWaterMark, w.config.RetryingLowWaterMark) {
			verdict.reason = admissionDeferralReasonGraphWritePressure
			verdict.failureClass = admissionGraphWriteTimeoutFailureClass
			verdict.depth = graphWriteTimeoutDepth.value
			return w.observed(ctx, verdict), nil
		}
	}

	if w.config.HighWaterMark > 0 {
		total, err := w.queueDepthTotal(ctx)
		if err != nil {
			return admissionVerdict{}, fmt.Errorf("read reducer admission queue depth: %w", err)
		}
		verdict.observe(total)
		if total.value >= w.config.HighWaterMark {
			verdict.reason = admissionDeferralReasonHighWater
			verdict.depth = total.value
			return w.observed(ctx, verdict), nil
		}
	}

	if sourceSystem != "" && w.sourceLegEnabled() {
		decision, err := w.sourceDecision(ctx, sourceSystem)
		if err != nil {
			return admissionVerdict{}, err
		}
		if decision.Slowed {
			verdict.reason = workflow.SourceBackpressureReasonSourceBehind
			verdict.retryAfter = decision.RetryAfter
		}
	}

	return w.observed(ctx, verdict), nil
}

// observe folds one depth read into the verdict.
func (v *admissionVerdict) observe(read admissionHeldValue[int64]) {
	if !v.used || read.readAt.Before(v.readAt) {
		v.readAt = read.readAt
	}
	v.seq = max(v.seq, read.seq)
	v.used = true
}

// observed records the age of the depth the verdict used and returns it.
func (w reducerAdmissionWriter) observed(ctx context.Context, verdict admissionVerdict) admissionVerdict {
	if w.holder != nil && verdict.used {
		w.holder.recordAge(ctx, verdict.readAt)
	}
	return verdict
}

// graphWriteTimeoutDepth reads the graph-write-pressure signal through the
// shared holder, or straight from the depth reader when there is none.
func (w reducerAdmissionWriter) graphWriteTimeoutDepth(ctx context.Context) (admissionHeldValue[int64], error) {
	if w.holder != nil {
		return w.holder.read(ctx, telemetry.ReducerAdmissionDepthSignalGraphWriteTimeout, w.holder.graphWriteTimeout)
	}
	readAt := w.clock()()
	depth, err := w.depthReader.ReducerGraphWriteTimeoutDepth(ctx)
	if err != nil {
		return admissionHeldValue[int64]{}, fmt.Errorf("read graph-write-timeout depth: %w", err)
	}
	return admissionHeldValue[int64]{value: depth, readAt: readAt}, nil
}

// queueDepthTotal reads the reducer total of QueueDepths (the high-water
// signal) through the shared holder, or straight from the depth reader when
// there is none. The definition of queue depth is the reader's; the gate only
// sums the reducer stage.
func (w reducerAdmissionWriter) queueDepthTotal(ctx context.Context) (admissionHeldValue[int64], error) {
	if w.holder != nil {
		return w.holder.read(ctx, telemetry.ReducerAdmissionDepthSignalQueue, w.holder.queueDepth)
	}
	readAt := w.clock()()
	depths, err := w.depthReader.QueueDepths(ctx)
	if err != nil {
		return admissionHeldValue[int64]{}, fmt.Errorf("read queue depths: %w", err)
	}
	var total int64
	for _, count := range depths["reducer"] {
		total += count
	}
	return admissionHeldValue[int64]{value: total, readAt: readAt}, nil
}

// deferralSleep returns how long a deferred waiter sleeps before its next
// decision: until the depth it deferred on leaves the holder's window (the
// poll interval without a holder), never past the time left before the max
// wait, so the timeout still fires on time.
func (w reducerAdmissionWriter) deferralSleep(ctx context.Context, verdict admissionVerdict, waitStart time.Time) time.Duration {
	sleep := w.config.PollInterval
	if w.holder != nil {
		sleep = w.holder.deferred(ctx, verdict.seq, verdict.readAt)
	}
	if w.config.MaxWait > 0 {
		left := w.config.MaxWait - w.clock()().Sub(waitStart)
		if left < 0 {
			left = 0
		}
		sleep = min(sleep, left)
	}
	return sleep
}
