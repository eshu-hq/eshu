// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ImpactNotReadyFailureClass classifies a deferral of a security alert
// reconciliation that has owned dependency evidence but no impact finding while
// the supply_chain_impact work item of the SAME scope generation is still in
// flight (#8827).
//
// Both domains are enqueued from the same scope generation, and the
// reconciliation can only match an alert once its producer has written an
// impact finding. The two share the default (scope, scope id) conflict key, but
// that fence only holds while the producer is claimed or running: while the
// producer is pending or retrying (a backoff, or a non-counting readiness wait
// of its own) the reconciliation is claimable, runs, and used to spend one of
// its counting attempts on a wait it could not avoid. Three such attempts made
// the handler give up and write unmatched rows although impact evidence was
// about to exist.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the claim keeps
// attempt_count for a retrying row in this class. Freezing the count also
// freezes the handler's own cap, so the wait is bounded by ELAPSED TIME
// (crossscope.ProducerReadinessMaxWait, measured from the producer row's
// last-workable time), never by an attempt comparison.
const ImpactNotReadyFailureClass = "security_alert_reconciliation_impact_not_ready"

// Outcomes recorded on eshu_dp_security_alert_reconciliation_impact_wait_total
// and named as impact_wait= on the result summary. The set is closed.
const (
	// ImpactWaitDeferredInFlight means the impact work item of the same scope
	// generation was pending, retrying, claimed, or running inside the bound:
	// the reconciliation deferred with ImpactNotReadyFailureClass and wrote
	// nothing. No attempt was spent.
	ImpactWaitDeferredInFlight = "deferred_in_flight"
	// ImpactWaitGaveUpUnmatched means the handler reached its own attempt cap
	// with no impact work in flight and wrote its unmatched decisions anyway.
	ImpactWaitGaveUpUnmatched = "gave_up_unmatched"
	// ImpactWaitGaveUpBoundExceeded means the handler wrote its unmatched
	// decisions at the attempt cap because the in-flight impact work had shown
	// no sign of life for crossscope.ProducerReadinessMaxWait.
	ImpactWaitGaveUpBoundExceeded = "gave_up_bound_exceeded"
)

// ImpactProducerState counts the supply_chain_impact work items of one scope
// generation by queue status.
type ImpactProducerState struct {
	Pending  int
	Retrying int
	Claimed  int
	Running  int
	// Total counts every status, terminal ones included. Zero means no impact
	// work was ever enqueued for the generation.
	Total int
	// NewestInFlightWorkableAt is the latest last-workable time across the
	// in-flight rows: GREATEST(COALESCE(reopened_at, created_at),
	// COALESCE(last_attempt_at, created_at), COALESCE(visible_at, created_at),
	// COALESCE(claim_until, created_at)). It can lie in the future during a
	// retry backoff or a live lease, and is zero when no row is in flight.
	NewestInFlightWorkableAt time.Time
}

// InFlight reports how many impact work items are not yet terminal.
func (s ImpactProducerState) InFlight() int {
	return s.Pending + s.Retrying + s.Claimed + s.Running
}

// ImpactProducerReadiness reports the state of the supply_chain_impact work of
// one scope generation. An error means the question could not be answered and
// must never be read as ready or not ready. A nil port disables the fence.
type ImpactProducerReadiness interface {
	ImpactProducerState(ctx context.Context, scopeID, generationID string) (ImpactProducerState, error)
}

// impactInFlightError defers the reconciliation, before any write, until the
// impact work of its scope generation drains.
type impactInFlightError struct {
	scopeID      string
	generationID string
	packageID    string
}

func (e impactInFlightError) Error() string {
	subject := ""
	if strings.TrimSpace(e.packageID) != "" {
		subject = " for " + strings.TrimSpace(e.packageID)
	}
	return fmt.Sprintf(
		"security alert reconciliation waiting for supply chain impact work in flight%s in scope %s generation %s",
		subject, e.scopeID, e.generationID,
	)
}

// Retryable reports that the deferral must be retried once the impact work drains.
func (impactInFlightError) Retryable() bool { return true }

// ExpectedWait marks the deferral as a designed wait so the reducer records it
// as deferred at WARN, not failed at ERROR (#8826).
func (impactInFlightError) ExpectedWait() bool { return true }

// FailureClass returns the non-counting readiness class for the queue.
func (impactInFlightError) FailureClass() string { return ImpactNotReadyFailureClass }

// impactWaitDecision is the outcome of resolveImpactWait for one pass.
type impactWaitDecision struct {
	// giveUp is the closed impact_wait outcome when the handler stops waiting
	// and writes its unmatched decisions; empty when it did not give up.
	giveUp string
}

// summarySuffix names the give-up reason on the result summary, empty when the
// handler did not give up.
func (d impactWaitDecision) summarySuffix() string {
	if d.giveUp == "" {
		return ""
	}
	return " impact_wait=" + d.giveUp
}

// subSignals adds the give-up flag to the result's sub signals, which the
// reducer logs as sub_signal_impact_wait_gave_up on the success line, so a
// give-up is findable from the execution log and not only from the WARN line.
// It returns base unchanged when the handler did not give up.
func (d impactWaitDecision) subSignals(base map[string]float64) map[string]float64 {
	if d.giveUp == "" {
		return base
	}
	out := make(map[string]float64, len(base)+1)
	for key, value := range base {
		out[key] = value
	}
	out["impact_wait_gave_up"] = 1
	return out
}

// resolveImpactWait decides what a reconciliation with decisions waiting on
// impact evidence does: defer without spending an attempt while the impact
// work of its scope generation is in flight inside the bound, defer on the
// counting path below the handler cap, or give up visibly at the cap. It
// returns a zero decision and a nil error when nothing waits on impact.
func (h SecurityAlertReconciliationHandler) resolveImpactWait(
	ctx context.Context,
	intent reducercontract.Intent,
	decisions []SecurityAlertReconciliationDecision,
) (impactWaitDecision, error) {
	if !hasPendingImpactDecision(intent, decisions) {
		return impactWaitDecision{}, nil
	}
	boundExceeded := false
	if h.ImpactProducerReadiness != nil {
		state, err := h.ImpactProducerReadiness.ImpactProducerState(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			// A torn connection is the retryable fact_load_transient class,
			// which still counts against the retry budget; every other error
			// is returned as is. Neither is ever converted into the
			// non-counting class: that would retry forever without surfacing.
			return impactWaitDecision{}, fmt.Errorf(
				"check supply chain impact producer readiness: %w", factload.ClassifyFactLoadError(err))
		}
		if state.InFlight() > 0 {
			now := h.now()
			// A zero anchor is unknown elapsed time, not infinite: keep waiting.
			anchor := state.NewestInFlightWorkableAt
			if anchor.IsZero() || now.Sub(anchor) < crossscope.ProducerReadinessMaxWait {
				h.recordImpactWait(ctx, ImpactWaitDeferredInFlight)
				h.logImpactWait(ctx, slog.LevelInfo,
					"security alert reconciliation deferred: supply chain impact in flight for the same scope generation",
					intent, ImpactWaitDeferredInFlight, state, now)
				return impactWaitDecision{}, impactInFlightError{
					scopeID:      intent.ScopeID,
					generationID: intent.GenerationID,
					packageID:    firstUnmatchedPackageWithDependency(decisions),
				}
			}
			boundExceeded = true
		}
	}
	if intent.AttemptCount < pendingImpactSecurityAlertReconciliationMaxAttempts {
		return impactWaitDecision{}, retryableSecurityAlertReconciliationEvidenceError{
			packageID: firstUnmatchedPackageWithDependency(decisions),
		}
	}
	outcome := ImpactWaitGaveUpUnmatched
	if boundExceeded {
		outcome = ImpactWaitGaveUpBoundExceeded
	}
	h.recordImpactWait(ctx, outcome)
	h.logImpactWait(ctx, slog.LevelWarn,
		"security alert reconciliation gave up waiting for impact evidence and wrote unmatched decisions",
		intent, outcome, ImpactProducerState{}, h.now())
	return impactWaitDecision{giveUp: outcome}, nil
}

// hasPendingImpactDecision reports whether any decision waits on impact
// evidence: owned dependency evidence, no impact finding, on an
// impact-producing trigger. It does not look at the attempt count.
func hasPendingImpactDecision(
	intent reducercontract.Intent,
	decisions []SecurityAlertReconciliationDecision,
) bool {
	if !impactProducingSecurityAlertReconciliation(intent) {
		return false
	}
	for _, decision := range decisions {
		if decision.Status == SecurityAlertReconciliationUnmatched &&
			strings.TrimSpace(decision.DependencyEvidenceID) != "" &&
			strings.TrimSpace(decision.ImpactEvidenceID) == "" {
			return true
		}
	}
	return false
}

func (h SecurityAlertReconciliationHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h SecurityAlertReconciliationHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h SecurityAlertReconciliationHandler) recordImpactWait(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.SecurityAlertReconciliationImpactWait == nil {
		return
	}
	h.Instruments.SecurityAlertReconciliationImpactWait.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSecurityAlertReconciliation)),
		telemetry.AttrOutcome(outcome),
	))
}

// logImpactWait writes the defer (INFO) or give-up (WARN) line. It carries
// attempt_count for the give-up, where the count is live; a non-counting defer
// freezes it, so the defer line reports producer state and elapsed time
// against producer_max_wait instead.
func (h SecurityAlertReconciliationHandler) logImpactWait(
	ctx context.Context,
	level slog.Level,
	message string,
	intent reducercontract.Intent,
	outcome string,
	state ImpactProducerState,
	now time.Time,
) {
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyDomain, string(intent.Domain)),
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String("impact_wait_outcome", outcome),
	}
	if outcome != ImpactWaitDeferredInFlight {
		attrs = append(attrs,
			slog.Int("attempt_count", intent.AttemptCount),
			slog.Int("handler_attempt_cap", pendingImpactSecurityAlertReconciliationMaxAttempts),
		)
	} else {
		attrs = append(attrs,
			slog.String(telemetry.LogKeyFailureClass, ImpactNotReadyFailureClass),
			slog.Int("impact_pending", state.Pending),
			slog.Int("impact_retrying", state.Retrying),
			slog.Int("impact_claimed", state.Claimed),
			slog.Int("impact_running", state.Running),
			slog.Duration("producer_max_wait", crossscope.ProducerReadinessMaxWait),
		)
		if !state.NewestInFlightWorkableAt.IsZero() {
			attrs = append(attrs, slog.Duration("producer_elapsed", now.Sub(state.NewestInFlightWorkableAt)))
		}
	}
	h.logger().LogAttrs(ctx, level, message, attrs...)
}
