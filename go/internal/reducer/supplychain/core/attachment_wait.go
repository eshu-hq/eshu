// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SBOMAttachmentNotReadyFailureClass classifies a deferral of a
// supply_chain_impact pass while the sbom_attestation_attachment work item of
// the SAME scope generation is still in flight (#11105).
//
// The projector enqueues both intents from one scope generation. The impact
// pass reads the attachment fact its sibling writes, and nothing re-runs the
// pass when the attachment commits later: sbom_attestation_attachment is not a
// completion-catalog producer. The two rows share the default (scope, scope
// id) conflict key, but that fence holds only while the attachment is claimed
// or running. While it is pending or retrying (a backoff with a future
// visible_at) the impact row is claimable, and a pass that ran then published
// findings without the attachment-backed ones until an unrelated trigger.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go) and parked as a
// producer-terminal wait narrowed to sbom_attestation_attachment, so the
// attachment's terminal Ack reopens the pass at once. The wait is bounded by
// ELAPSED TIME (crossscope.ProducerReadinessMaxWait from the attachment row's
// last-workable time), never by an attempt comparison.
const SBOMAttachmentNotReadyFailureClass = "supply_chain_impact_sbom_attachment_not_ready"

// Outcomes recorded on eshu_dp_supply_chain_impact_attachment_wait_total. The
// set is closed. A pass that finds no attachment work, or every attachment row
// succeeded, records nothing.
const (
	// AttachmentWaitDeferredInFlight means the same scope generation's
	// attachment work was pending, retrying, claimed, or running inside the
	// bound: the pass deferred with SBOMAttachmentNotReadyFailureClass before
	// loading any evidence and wrote nothing. No attempt was spent.
	AttachmentWaitDeferredInFlight = "deferred_in_flight"
	// AttachmentWaitProducerFailed means no attachment work was in flight but
	// at least one attachment row dead-lettered or failed: the pass proceeds
	// without that attachment's evidence.
	AttachmentWaitProducerFailed = "producer_failed"
	// AttachmentWaitBoundExceeded means attachment work was in flight but had
	// shown no sign of life for crossscope.ProducerReadinessMaxWait: the pass
	// proceeds without waiting longer.
	AttachmentWaitBoundExceeded = "bound_exceeded"
)

// SBOMAttachmentReadiness reports the sbom_attestation_attachment work state of
// one scope generation, with the same per-status counts and last-workable
// anchor the #8827 reconciliation fence reads for supply_chain_impact. An error
// means the question could not be answered and is never read as ready or not
// ready. A nil port disables the wait.
type SBOMAttachmentReadiness interface {
	SBOMAttestationAttachmentState(ctx context.Context, scopeID, generationID string) (securityalert.ImpactProducerState, error)
}

// sbomAttachmentInFlightError defers the impact pass, before any evidence load
// or write, until the attachment work of its scope generation is terminal.
type sbomAttachmentInFlightError struct {
	scopeID      string
	generationID string
}

func (e sbomAttachmentInFlightError) Error() string {
	return fmt.Sprintf(
		"supply chain impact waiting for sbom attestation attachment work in flight in scope %s generation %s",
		e.scopeID, e.generationID,
	)
}

// Retryable reports that the deferral must be retried once the attachment
// work is terminal.
func (sbomAttachmentInFlightError) Retryable() bool { return true }

// ExpectedWait marks the deferral as a designed wait so the reducer records it
// as deferred at WARN, not failed at ERROR (#8826).
func (sbomAttachmentInFlightError) ExpectedWait() bool { return true }

// FailureClass returns the non-counting readiness class for the queue.
func (sbomAttachmentInFlightError) FailureClass() string { return SBOMAttachmentNotReadyFailureClass }

// awaitOwnSBOMAttachment is the #11105 sibling wait. It runs on every attempt
// BEFORE the evidence load (and so before the #5709 floor sample), for the same
// reason the #8827 fence probes early: the attachment writer commits its fact
// inside its own handler and the queue acks succeeded only afterwards, so "no
// attachment row in flight" read before the load means every committed
// attachment is visible to the load. Read after the load, an attachment that
// committed in between would be absent from this pass and nothing would re-run
// it.
//
// It returns nil to proceed and sbomAttachmentInFlightError to defer. A probe
// error is returned through factload.ClassifyFactLoadError, a COUNTING class:
// converting it to the non-counting class would retry forever without
// surfacing.
func (h SupplyChainImpactHandler) awaitOwnSBOMAttachment(ctx context.Context, intent reducercontract.Intent) error {
	if h.AttachmentReadiness == nil {
		return nil
	}
	state, err := h.AttachmentReadiness.SBOMAttestationAttachmentState(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return fmt.Errorf("check sbom attestation attachment readiness: %w", factload.ClassifyFactLoadError(err))
	}
	if state.Total == 0 {
		return nil
	}
	now := h.evaluationNow()
	if state.InFlight() > 0 {
		// A zero anchor is unknown elapsed time, not infinite: keep waiting.
		anchor := state.NewestInFlightWorkableAt
		if anchor.IsZero() || now.Sub(anchor) < crossscope.ProducerReadinessMaxWait {
			h.recordAttachmentWait(ctx, AttachmentWaitDeferredInFlight)
			h.logAttachmentWait(ctx, slog.LevelInfo,
				"supply chain impact deferred: sbom attestation attachment in flight for the same scope generation",
				intent, AttachmentWaitDeferredInFlight, state, now)
			return sbomAttachmentInFlightError{scopeID: intent.ScopeID, generationID: intent.GenerationID}
		}
		h.recordAttachmentWait(ctx, AttachmentWaitBoundExceeded)
		h.logAttachmentWait(ctx, slog.LevelWarn,
			"supply chain impact stopped waiting for an sbom attestation attachment past the bound and ran without it",
			intent, AttachmentWaitBoundExceeded, state, now)
		return nil
	}
	if state.Failed > 0 {
		h.recordAttachmentWait(ctx, AttachmentWaitProducerFailed)
		h.logAttachmentWait(ctx, slog.LevelWarn,
			"supply chain impact ran without a dead-lettered or failed sbom attestation attachment",
			intent, AttachmentWaitProducerFailed, state, now)
	}
	return nil
}

// beforeEvidenceLoad runs the decisions Handle makes before it issues a
// fencing token or loads any evidence: the #11197 supports-readiness gate, which
// defers the pass non-counting while the holder reads' table is not ready (it
// runs first because the manifest marker below already reads repository
// holders); a #9874 git manifest marker reopens holders and writes no findings;
// and the #11105 attachment wait defers a pass whose own attachment is still in
// flight. handled reports that Handle must return result and err as they are.
func (h SupplyChainImpactHandler) beforeEvidenceLoad(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, bool, error) {
	if err := h.requireSupportsReady(ctx); err != nil {
		return reducercontract.Result{}, true, err
	}
	if result, handled, err := h.handleManifestTrigger(ctx, intent); handled {
		return result, true, err
	}
	if err := h.awaitOwnSBOMAttachment(ctx, intent); err != nil {
		return reducercontract.Result{}, true, err
	}
	return reducercontract.Result{}, false, nil
}

func (h SupplyChainImpactHandler) recordAttachmentWait(ctx context.Context, outcome string) {
	if h.Instruments == nil || h.Instruments.SupplyChainImpactAttachmentWait == nil {
		return
	}
	h.Instruments.SupplyChainImpactAttachmentWait.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrDomain(string(reducercontract.DomainSupplyChainImpact)),
		telemetry.AttrOutcome(outcome),
	))
}

// logAttachmentWait writes the defer (INFO) or proceed-without (WARN) line with
// the attachment sibling's counts and the elapsed time against the bound. It
// never carries attempt_count, which the non-counting class freezes.
func (h SupplyChainImpactHandler) logAttachmentWait(
	ctx context.Context,
	level slog.Level,
	message string,
	intent reducercontract.Intent,
	outcome string,
	state securityalert.ImpactProducerState,
	now time.Time,
) {
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []slog.Attr{
		slog.String(telemetry.LogKeyDomain, string(reducercontract.DomainSupplyChainImpact)),
		slog.String(telemetry.LogKeyScopeID, intent.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, intent.GenerationID),
		slog.String("attachment_wait_outcome", outcome),
		slog.String("producer_domain", string(reducercontract.DomainSBOMAttestationAttachment)),
		slog.Int("attachment_pending", state.Pending),
		slog.Int("attachment_retrying", state.Retrying),
		slog.Int("attachment_claimed", state.Claimed),
		slog.Int("attachment_running", state.Running),
		slog.Int("attachment_succeeded", state.Succeeded),
		slog.Int("attachment_failed", state.Failed),
		slog.Int("attachment_total", state.Total),
		slog.Duration("producer_max_wait", crossscope.ProducerReadinessMaxWait),
	}
	if outcome == AttachmentWaitDeferredInFlight {
		attrs = append(attrs, slog.String(telemetry.LogKeyFailureClass, SBOMAttachmentNotReadyFailureClass))
	}
	if !state.NewestInFlightWorkableAt.IsZero() {
		// Negative while a retry backoff or a live lease lies in the future.
		attrs = append(attrs, slog.Duration("producer_elapsed", now.Sub(state.NewestInFlightWorkableAt)))
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}
