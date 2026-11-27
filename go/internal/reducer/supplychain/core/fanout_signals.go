// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The fan-out's operator signals: its span, its counter outcomes, its log
// lines, and the result sub-signals.

// Pass-level counter outcomes that are not a per-holder result. They live
// with the signals because they are the counter's outcome label set, not the
// fan-out's read or reopen logic.
const (
	fanOutOutcomeCrashRecovery = "crash_recovery"
	fanOutOutcomeDeferred      = "deferred"
	fanOutOutcomeReadError     = "read_error"
	fanOutOutcomeReadTimeout   = "read_timeout"
	fanOutOutcomeReopenError   = "reopen_error"
	fanOutOutcomeStateError    = "state_error"
	// fanOutReadOutcomeOK is the read-duration histogram's outcome for a
	// statement that returned (#10643); the counter has no ok outcome.
	fanOutReadOutcomeOK = "ok"
)

// fanOutResult is what one pass's fan-out did, for sub-signals and the log.
type fanOutResult struct {
	// cause is the committing generation's evidence family (fanOutCause),
	// carried on every counter outcome this pass emits.
	cause         string
	holders       int
	reopened      int
	replayFlagged int
	pendingNoop   int
	enqueued      int
	// stampWidened counts holders whose queued or already-flagged item got
	// this change merged into its stamp (#10972).
	stampWidened int
	// shadowSkipped counts holders that met every PR1 freshness condition
	// and still reopened: the would-skip share PR2 decides on (#11613). It
	// is counted apart from pendingNoop so the Gate-B stock metric cannot
	// absorb it silently.
	shadowSkipped int
	// prefiltered counts holders the queued-holder prefilter dropped before
	// the cap slice: their item was already pending or retrying (#10972).
	prefiltered int
	// deadLetterReopened, deadLetterSkipped, and noActiveGeneration count the
	// #11147 holder outcomes for the summary line and sub-signals; their
	// counter series are written per holder (recordHolderReopen) or with the
	// pass (emitFanOut).
	deadLetterReopened int
	deadLetterSkipped  int
	noActiveGeneration int
	// holderStatements, splitChunks, hotIDs, and maxIDMassRows record what
	// adaptive splitting did (#10643): statements run, chunks that split,
	// singleton statements (hot by mass, or isolated by packing/unknown
	// mass — read hotIDs as singletons), and the largest measured per-id
	// mass.
	holderStatements int
	splitChunks      int
	hotIDs           int
	maxIDMassRows    int64
	// hotLaneStatements and coldLaneStatements count the executed holder
	// statements per #11520 H2 lane, for the sub-signals, span, and log.
	hotLaneStatements  int
	coldLaneStatements int
	// keyRangedIDs and keyRangeStatements record the hot singleton ids read
	// as fact_id key ranges and the key-range statements they ran (#10643).
	keyRangedIDs       int
	keyRangeStatements int
	// deferred is the number of holders still owed after this claim, a gauge
	// the sub-signal reports. newlyDeferred is the part that was not already
	// owed when the claim started, which is what the counter adds, so draining
	// one backlog across several claims counts each holder once.
	deferred      int
	newlyDeferred int
	crashRecovery bool
	// replayCleared is true when the pass proved its rewrite clear of the
	// flagged change and its replay flag was cleared (#9740), so the ack
	// succeeds instead of rerunning it.
	replayCleared bool
	inFlight      inFlightNarrowing // the #10972 D4b in-flight arm
}

func (h SupplyChainImpactHandler) startFanOutSpan(ctx context.Context, intent reducercontract.Intent) (context.Context, trace.Span) {
	if h.Tracer == nil {
		// A no-op span: ending it must never end the caller's span.
		return ctx, trace.SpanFromContext(context.Background())
	}
	return h.Tracer.Start(ctx, telemetry.SpanSupplyChainImpactFanOut, trace.WithAttributes(
		attribute.String("scope_id", intent.ScopeID),
		attribute.String("generation_id", intent.GenerationID),
	))
}

// countSupplyChainImpactPass counts one findings pass by how it ran (#9779).
// It fires once per pass that completes its evidence load, whatever the
// downstream stages do, so scoped and full stay comparable across failures.
func (h SupplyChainImpactHandler) countSupplyChainImpactPass(ctx context.Context, mode string) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactPassTotal == nil {
		return
	}
	h.Instruments.SupplyChainImpactPassTotal.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("mode", mode),
	))
}

func (h SupplyChainImpactHandler) countFanOut(ctx context.Context, cause string, outcome string, n int) {
	if n <= 0 || h.Instruments == nil || h.Instruments.SupplyChainImpactFanOut == nil {
		return
	}
	if cause == "" {
		cause = fanOutCauseOther
	}
	h.Instruments.SupplyChainImpactFanOut.Add(ctx, int64(n), metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", outcome),
		attribute.String("cause", cause),
	))
}

// emitFanOut records the pass's counters and its one summary log line. The
// per-holder detail is a debug line (logFanOutHolder) so a 256-holder claim
// does not write 256 info lines.
func (h SupplyChainImpactHandler) emitFanOut(ctx context.Context, intent reducercontract.Intent, r fanOutResult) {
	h.countFanOut(ctx, r.cause, string(FanOutReopened), r.reopened)
	h.countFanOut(ctx, r.cause, string(FanOutReplayFlagged), r.replayFlagged)
	h.countFanOut(ctx, r.cause, string(FanOutPendingNoop), r.pendingNoop)
	h.countFanOut(ctx, r.cause, string(FanOutEnqueued), r.enqueued)
	h.countFanOut(ctx, r.cause, string(FanOutStampWidened), r.stampWidened)
	h.countFanOut(ctx, r.cause, string(FanOutShadowWouldSkip), r.shadowSkipped)
	h.countFanOut(ctx, r.cause, fanOutOutcomePrefilteredPending, r.prefiltered)
	h.countFanOut(ctx, r.cause, string(FanOutNoActiveGeneration), r.noActiveGeneration)
	h.countFanOut(ctx, r.cause, fanOutOutcomeDeferred, r.newlyDeferred)
	if r.crashRecovery {
		h.countFanOut(ctx, r.cause, fanOutOutcomeCrashRecovery, 1)
	}
	h.emitInFlightNarrowing(ctx, r.cause, r.inFlight)
	if h.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("cause", r.cause),
		slog.String("scope_id", intent.ScopeID),
		slog.String("generation_id", intent.GenerationID),
		slog.String("intent_id", intent.IntentID),
		slog.Int("holders", r.holders),
		slog.Int("reopened", r.reopened),
		slog.Int("replay_flagged", r.replayFlagged),
		slog.Int("pending_noop", r.pendingNoop),
		slog.Int("stamp_widened", r.stampWidened),
		slog.Int("shadow_would_skip", r.shadowSkipped),
		slog.Int("prefiltered_pending", r.prefiltered),
		slog.Int("enqueued", r.enqueued),
		slog.Int("dead_letter_reopened", r.deadLetterReopened),
		slog.Int("dead_letter_skipped", r.deadLetterSkipped),
		slog.Int("no_active_generation", r.noActiveGeneration),
		slog.Int("deferred", r.deferred),
		slog.Bool("crash_recovery", r.crashRecovery),
		slog.Int("holder_statements", r.holderStatements),
		slog.Int("holder_statements_hot", r.hotLaneStatements),
		slog.Int("holder_statements_cold", r.coldLaneStatements),
		slog.Int("split_chunks", r.splitChunks),
		slog.Int("hot_ids", r.hotIDs),
		slog.Int64("max_id_mass_rows", r.maxIDMassRows),
		slog.Int("key_ranged_ids", r.keyRangedIDs),
		slog.Int("holder_key_range_statements", r.keyRangeStatements),
	}
	h.Logger.InfoContext(ctx, "supply chain impact holder fan-out", append(attrs, r.inFlight.inFlightLogAttrs()...)...)
}

func (h SupplyChainImpactHandler) logFanOutHolder(ctx context.Context, intent reducercontract.Intent, holder string, outcome FanOutOutcome) {
	if h.Logger == nil {
		return
	}
	h.Logger.DebugContext(ctx, "supply chain impact holder reopened",
		slog.String("source_scope_id", intent.ScopeID),
		slog.String("source_generation_id", intent.GenerationID),
		slog.String("holder_scope_id", holder),
		slog.String("outcome", string(outcome)),
	)
}

// readFailureOutcome maps a holder read error to its counter outcome.
func readFailureOutcome(err error) string {
	if errors.Is(err, ErrFanOutReadTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return fanOutOutcomeReadTimeout
	}
	return fanOutOutcomeReadError
}

// countFanOutRead counts one failed fan-out read with its read site (#10564
// H): the counter names the read that failed, so the next regression points
// at its statement instead of a bare read_timeout. site must be one of the
// telemetry.FanOutReadSite* constants. Only read outcomes (read_error,
// read_timeout) carry the site; every other outcome keeps the counter's
// existing label set.
func (h SupplyChainImpactHandler) countFanOutRead(ctx context.Context, cause string, err error, site string) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactFanOut == nil {
		return
	}
	if cause == "" {
		cause = fanOutCauseOther
	}
	h.Instruments.SupplyChainImpactFanOut.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		attribute.String("outcome", readFailureOutcome(err)),
		attribute.String("cause", cause),
		telemetry.AttrFanOutReadSite(site),
	))
}

func (r *fanOutResult) record(outcome FanOutOutcome) {
	switch outcome {
	case FanOutReopened:
		r.reopened++
	case FanOutReplayFlagged:
		r.replayFlagged++
	case FanOutEnqueued:
		r.enqueued++
	case FanOutStampWidened:
		r.stampWidened++
	case FanOutShadowWouldSkip:
		r.shadowSkipped++
	case FanOutDeadLetterReopened:
		r.deadLetterReopened++
	case FanOutDeadLetterSkipped:
		r.deadLetterSkipped++
	case FanOutNoActiveGeneration:
		r.noActiveGeneration++
	default:
		r.pendingNoop++
	}
}

// mergeSubSignals adds the fan-out counts to a result's sub-signals.
func (r fanOutResult) mergeSubSignals(subSignals map[string]float64) {
	subSignals["fanout_holders"] = float64(r.holders)
	subSignals["fanout_prefiltered_pending"] = float64(r.prefiltered)
	subSignals["fanout_holder_statements"] = float64(r.holderStatements)
	subSignals["fanout_holder_statements_hot"] = float64(r.hotLaneStatements)
	subSignals["fanout_holder_statements_cold"] = float64(r.coldLaneStatements)
	subSignals["fanout_hot_ids"] = float64(r.hotIDs)
	subSignals["fanout_max_id_mass_rows"] = float64(r.maxIDMassRows)
	subSignals["fanout_key_ranged_ids"] = float64(r.keyRangedIDs)
	subSignals["fanout_holder_key_range_statements"] = float64(r.keyRangeStatements)
	subSignals["fanout_reopened"] = float64(r.reopened)
	subSignals["fanout_replay_flagged"] = float64(r.replayFlagged)
	subSignals["fanout_stamp_widened"] = float64(r.stampWidened)
	subSignals["fanout_shadow_would_skip"] = float64(r.shadowSkipped)
	subSignals["fanout_dead_letter_reopened"] = float64(r.deadLetterReopened)
	subSignals["fanout_dead_letter_skipped"] = float64(r.deadLetterSkipped)
	subSignals["fanout_deferred"] = float64(r.deferred)
	subSignals["fanout_replay_cleared"] = boolSignal(r.replayCleared)
}

// noteKeyRangeStatement reports one statement of a key-range read that
// failed, ran at or over fanOutHolderSlowStatement, or was the fallback
// (#10643 PR 2, arbiter rulings 10643l decision 3 and 10643m). The member arm
// rides inside each key-range statement as its token arm, and one statement
// cannot be timed by arm, so the signal names the statement instead: one
// event on the fan-out span and one warn log line, each with the statement's
// kind, range index (1 to statements), range count, whether the lookup set
// held a non-CVE id (the token arm in play), whether the read fell back to
// the shipped statement for a lookup id over 1,024 bytes, the outcome and
// the duration. A fallback always reports, so an operator sees the long id
// that caused it. A fast key-range statement that succeeds reports nothing;
// the read-duration histogram already counts it.
func (h SupplyChainImpactHandler) noteKeyRangeStatement(ctx context.Context, done HolderReadStatement, statements int, nonCVELookup, fallback bool) {
	if done.Err == nil && done.Duration < fanOutHolderSlowStatement && !fallback {
		return
	}
	outcome := fanOutReadOutcomeOK
	if done.Err != nil {
		outcome = readFailureOutcome(done.Err)
	}
	trace.SpanFromContext(ctx).AddEvent("holder_key_range_statement", trace.WithAttributes(
		attribute.String("kind", done.Kind),
		attribute.Int("range_index", done.Range),
		attribute.Int("range_count", statements),
		attribute.Bool("non_cve_lookup", nonCVELookup),
		attribute.Bool("fallback", fallback),
		attribute.String("outcome", outcome),
		attribute.Float64("duration_seconds", done.Duration.Seconds()),
	))
	if h.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
		slog.String("kind", done.Kind),
		slog.Int("range_index", done.Range),
		slog.Int("range_count", statements),
		slog.Bool("non_cve_lookup", nonCVELookup),
		slog.Bool("fallback", fallback),
		slog.String("outcome", outcome),
		slog.Float64("duration_seconds", done.Duration.Seconds()),
	}
	if done.Err != nil {
		attrs = append(attrs, slog.String("error", done.Err.Error()))
	}
	h.Logger.WarnContext(ctx, keyRangeStatementLogMessage, attrs...)
}

// observeFanOutRead records one holder-read statement's duration (#10643) by
// read kind (one of the telemetry.FanOutReadKind* constants) and outcome (ok,
// read_timeout, read_error), so an operator reads per-statement p95 against
// the 2 s bar and the timeout share at the 10 s guard per kind.
func (h SupplyChainImpactHandler) observeFanOutRead(ctx context.Context, kind string, elapsed time.Duration, err error) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactFanOutReadDuration == nil {
		return
	}
	outcome := fanOutReadOutcomeOK
	if err != nil {
		outcome = readFailureOutcome(err)
	}
	h.Instruments.SupplyChainImpactFanOutReadDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		telemetry.AttrFanOutReadKind(kind),
		attribute.String("outcome", outcome),
	))
}

// fanOutSiteTimes accumulates the time one fan-out pass spends in each of the
// closed telemetry.SupplyChainImpactFanOutSites (#10972). It observes only:
// no decision reads it. The reads that feed it run one after another on the
// handler's goroutine, so it needs no lock.
type fanOutSiteTimes struct {
	elapsed map[string]time.Duration
}

func newFanOutSiteTimes() *fanOutSiteTimes {
	return &fanOutSiteTimes{elapsed: make(map[string]time.Duration, 4)}
}

// addEarlyRead credits the load-end in-flight read (set A) to the in_flight_early
// site: it ran before the writer, so the fan-out reports its measured duration
// instead of timing it. A pass that took no set A read reports nothing.
func (t *fanOutSiteTimes) addEarlyRead(early inFlightAtLoadEnd) {
	if early.state == "" || early.state == inFlightEarlyReadSkipped {
		return
	}
	t.elapsed[telemetry.SupplyChainImpactFanOutSiteInFlightEarly] += early.duration
}

// begin starts timing one entry into site; the returned func adds the
// elapsed time to the site's total, so a site entered many times (one reopen
// per holder) reports its sum.
func (t *fanOutSiteTimes) begin(site string) func() {
	start := time.Now()
	return func() { t.elapsed[site] += time.Since(start) }
}

// finish records one histogram sample and one fan-out span attribute
// (<site>_seconds) per site the pass entered, in the closed set's order. A
// site the pass never entered reports nothing. It runs deferred, so a pass
// that fails inside a site still reports the time it spent there.
func (t *fanOutSiteTimes) finish(ctx context.Context, h SupplyChainImpactHandler, span trace.Span) {
	for _, site := range telemetry.SupplyChainImpactFanOutSites() {
		elapsed, entered := t.elapsed[site]
		if !entered {
			continue
		}
		span.SetAttributes(attribute.Float64(site+"_seconds", elapsed.Seconds()))
		if h.Instruments == nil || h.Instruments.SupplyChainImpactFanOutSiteDuration == nil {
			continue
		}
		h.Instruments.SupplyChainImpactFanOutSiteDuration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(
			telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
			telemetry.AttrSupplyChainImpactFanOutSite(site),
		))
	}
}

// setSpanAttributes puts the pass's fan-out counts on its span.
func (r fanOutResult) setSpanAttributes(span trace.Span) {
	span.SetAttributes(
		attribute.String("cause", r.cause),
		attribute.Int("holders", r.holders),
		attribute.Int("reopened", r.reopened),
		attribute.Int("replay_flagged", r.replayFlagged),
		attribute.Int("stamp_widened", r.stampWidened),
		attribute.Int("shadow_would_skip", r.shadowSkipped),
		attribute.Int("deferred", r.deferred),
		attribute.Int("prefiltered_pending", r.prefiltered),
		attribute.Int("holder_statements", r.holderStatements),
		attribute.Int("holder_statements_hot", r.hotLaneStatements),
		attribute.Int("holder_statements_cold", r.coldLaneStatements),
		attribute.Int("split_chunks", r.splitChunks),
		attribute.Int("hot_ids", r.hotIDs),
		attribute.Int64("max_id_mass_rows", r.maxIDMassRows),
		attribute.Int("key_ranged_ids", r.keyRangedIDs),
		attribute.Int("holder_key_range_statements", r.keyRangeStatements),
	)
}
