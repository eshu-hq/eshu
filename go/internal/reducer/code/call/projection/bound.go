// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// codeCallShortfallFailureClass names the failure class of the writer's
// shortfall park error. It is spelled out here instead of referencing the
// canonical reducer.SharedEdgeTargetNotReadyFailureClass because this
// package is a leaf below the reducer root and must never import it (see
// AGENTS.md); the writer pins the equality on its side
// (code_call_matched_count_test.go asserts the class), so a rename must
// update both.
const codeCallShortfallFailureClass = "shared_edge_target_not_ready"

// ShortfallStamper anchors the shortfall park's elapsed bound. The stamp is
// durable and COALESCE-once: the first park wins, so the bound measures from
// when the batch first shortfalled, not from the latest retry. The returned
// map carries every requested intent id the store still holds; a missing id
// (deleted mid-cycle) means the bound cannot be evaluated for this batch.
type ShortfallStamper interface {
	StampCodeCallShortfallOnce(ctx context.Context, intentIDs []string, now time.Time) (map[string]time.Time, error)
}

// isCodeCallShortfallParkError reports whether err is the writer's shortfall
// park: retryable under the shortfall failure class. An unverifiable batch
// (counting error: retryable but classless) is not a timing state, so it
// keeps the legacy behavior and never anchors the bound.
func isCodeCallShortfallParkError(err error) bool {
	if err == nil || !reducercontract.IsRetryable(err) {
		return false
	}
	var classed interface{ FailureClass() string }
	if !errors.As(err, &classed) {
		return false
	}
	return classed.FailureClass() == codeCallShortfallFailureClass
}

// oldestShortfallStamp returns the oldest stamp in the batch. A batch with
// any unstamped intent has no anchor yet, so the park defers (NULL defers).
func oldestShortfallStamp(active []sharedintent.Row, stamps map[string]time.Time) (time.Time, bool) {
	var oldest time.Time
	for _, row := range active {
		stamp, ok := stamps[row.IntentID]
		if !ok {
			return time.Time{}, false
		}
		if oldest.IsZero() || stamp.Before(oldest) {
			oldest = stamp
		}
	}
	return oldest, !oldest.IsZero()
}

// expireShortfallParkIfDue runs the #10947 bound at the writeActiveRows
// shortfall error return. It stamps the batch COALESCE-once and, when the
// oldest stamp reaches crossscope.ProducerReadinessMaxWait, dead-letters one
// sharedintent.UnroutableRow per active intent through the unroutable sink
// BEFORE the caller marks anything completed, then reports expired so the
// caller falls through to the shared completion path instead of returning
// the park error.
//
// Every other outcome reports expired=false with a nil error so the caller
// returns the original park error and the batch stays open: a non-shortfall
// error, no stamp port (NULL defers), a stamp failure (retried next cycle),
// a batch with no anchor yet (NULL defers), a bound not yet reached, or no
// unroutable sink (completing without the durable row would be the silent
// loss #5984 fixed). A failed dead-letter write is the one exit that fails
// the cycle with a new error before anything completes, so the batch re-runs
// and re-records idempotently.
func (r *Runner) expireShortfallParkIfDue(
	ctx context.Context,
	now time.Time,
	active []sharedintent.Row,
	writeErr error,
) (expired bool, err error) {
	if !isCodeCallShortfallParkError(writeErr) {
		return false, nil
	}
	intentIDs := make([]string, 0, len(active))
	for _, row := range active {
		intentIDs = append(intentIDs, row.IntentID)
	}
	stamper, ok := r.IntentReader.(ShortfallStamper)
	if !ok {
		return false, nil
	}
	stamps, stampErr := stamper.StampCodeCallShortfallOnce(ctx, intentIDs, now)
	if stampErr != nil {
		if r.Logger != nil {
			r.Logger.WarnContext(
				ctx,
				"code call projection failed to stamp the shortfall park; keeping the batch parked",
				slog.String("runner", "code_call_projection"),
				log.Domain(reducercontract.DomainCodeCalls),
				slog.Int("active_intents", len(active)),
				log.Err(stampErr),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}
		return false, nil
	}
	oldest, ok := oldestShortfallStamp(active, stamps)
	if !ok {
		return false, nil
	}
	if now.Sub(oldest) < crossscope.ProducerReadinessMaxWait {
		return false, nil
	}
	if r.UnroutableWriter == nil {
		if r.Logger != nil {
			r.Logger.WarnContext(
				ctx,
				"code call projection shortfall park expired with no unroutable sink; keeping the batch parked",
				slog.String("runner", "code_call_projection"),
				log.Domain(reducercontract.DomainCodeCalls),
				slog.Int("active_intents", len(active)),
				slog.Duration("elapsed", now.Sub(oldest)),
				slog.Duration("max_wait", crossscope.ProducerReadinessMaxWait),
				telemetry.PhaseAttr(telemetry.PhaseShared),
			)
		}
		return false, nil
	}
	unroutable := make([]sharedintent.UnroutableRow, 0, len(active))
	for _, row := range active {
		unroutable = append(unroutable, sharedintent.UnroutableRowForCodeCallShortfallWaitExceeded(row, codeCallRowEvidenceSource(row), now))
	}
	if werr := r.UnroutableWriter.WriteUnroutableIntents(ctx, unroutable); werr != nil {
		return false, fmt.Errorf("record code call shortfall dead letters: %w", werr)
	}
	r.recordShortfallWaitExceeded(ctx, len(active), now.Sub(oldest))
	return true, nil
}

// recordShortfallWaitExceeded emits the dead-letter half of the bound's
// telemetry: the shared dead-letter count under the bounded reason plus the
// stale-intents entry that keeps the terminal state apart from every drain,
// plus a WARN naming the batch shape for the 3 AM operator.
func (r *Runner) recordShortfallWaitExceeded(ctx context.Context, count int, elapsed time.Duration) {
	if r.Instruments != nil && r.Instruments.SharedEdgeUnroutableRows != nil {
		r.Instruments.SharedEdgeUnroutableRows.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrDomain(reducercontract.DomainCodeCalls),
			telemetry.AttrReason(sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded),
		))
	}
	worker.AcceptanceTelemetry{Instruments: r.Instruments, Logger: r.Logger}.RecordCodeCallShortfallWaitExceededIntents(
		ctx, "code_call_projection", string(reducercontract.DomainCodeCalls), count,
	)
	if r.Logger != nil {
		r.Logger.WarnContext(
			ctx,
			"code call projection shortfall park expired; dead-lettered the batch",
			slog.String("runner", "code_call_projection"),
			log.Domain(reducercontract.DomainCodeCalls),
			slog.Int("dead_lettered_intents", count),
			slog.Duration("elapsed", elapsed),
			slog.Duration("max_wait", crossscope.ProducerReadinessMaxWait),
			slog.String("unroutable_reason", sharedintent.UnroutableReasonCodeCallShortfallWaitExceeded),
			telemetry.PhaseAttr(telemetry.PhaseShared),
		)
	}
}

// recordShortfallTerminalCounts carries the write's submitted/matched counts
// onto the terminal submitted/matched series when the visit fails before
// completion (parked or dead-letter write failure). The success path records
// the same series in recordCodeCallTiming, so every terminal outcome is
// counted exactly once.
func (r *Runner) recordShortfallTerminalCounts(ctx context.Context, report sharedintent.WriteReport) {
	if r.Instruments == nil || report.SubmittedRows <= 0 {
		return
	}
	attrs := metric.WithAttributes(telemetry.AttrDomain(reducercontract.DomainCodeCalls))
	r.Instruments.CodeCallEdgeSubmittedRows.Add(ctx, int64(report.SubmittedRows), attrs)
	r.Instruments.CodeCallEdgeMatchedRows.Add(ctx, int64(report.MatchedRows), attrs)
}
