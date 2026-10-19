// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This file holds the handler's generation gates: the prior-generation
// retract decision and the #8611 freshness fence. Both answer "should this
// generation's run proceed" from generation state, never from row content,
// so they live together while Handle keeps only the two fence call sites.

package semantic

import (
	"context"
	"fmt"
	"log/slog"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

func (h Handler) shouldSkipSemanticRetract(ctx context.Context, intent reducercontract.Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for semantic retract: %w", err)
	}
	return !hasPrior, nil
}

// checkSemanticGenerationFreshness reports whether the intent's generation is
// still the scope's active generation (#8611). A nil GenerationCheck keeps
// the legacy behavior: every claimed intent runs to completion. A lookup
// error propagates wrapped with %w and is fail-closed: callers must never
// read it as supersession, because a pending generation
// (GenerationNotYetActiveError, retryable) is about to activate, not
// superseded, and skipping it would ack work nothing re-drives (#6686).
func (h Handler) checkSemanticGenerationFreshness(
	ctx context.Context,
	intent reducercontract.Intent,
) (bool, error) {
	if h.GenerationCheck == nil {
		return true, nil
	}
	current, err := h.GenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check semantic generation freshness: %w", err)
	}
	return current, nil
}

// fenceSemanticPreWrite runs the pre-write freshness fence (#8611): a
// superseded generation exits here, so a late run with SkipRetract=false can
// never retract repo-wide and upsert stale rows over the active generation's
// truth. It reports fenced=true with the superseded result, or fenced=false
// to proceed; a fence error propagates wrapped and fail-closed, never as
// supersession.
func (h Handler) fenceSemanticPreWrite(
	ctx context.Context,
	intent reducercontract.Intent,
) (reducercontract.Result, bool, error) {
	fresh, err := h.checkSemanticGenerationFreshness(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, false, err
	}
	if !fresh {
		logSemanticGenerationSuperseded(ctx, intent, semanticFencePhasePreWrite)
		return semanticSupersededResult(intent, 0), true, nil
	}
	return reducercontract.Result{}, false, nil
}

// resolveSemanticUnmatchedOutcome re-checks freshness before the
// identity-divergence failure (#8611): unmatched uids on a generation
// superseded mid-run are nodes the newer generation legitimately removed,
// not a derivation defect, so the run exits superseded carrying the write's
// CanonicalWrites. The completion log and the unmatched-uid WARN already
// emitted before this runs, so the operator keeps proof of what landed. A
// still-current generation keeps the #8587 poison-pill failure; a fence
// error propagates fail-closed.
func (h Handler) resolveSemanticUnmatchedOutcome(
	ctx context.Context,
	intent reducercontract.Intent,
	writeResult EntityWriteResult,
) (reducercontract.Result, error) {
	fresh, err := h.checkSemanticGenerationFreshness(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if !fresh {
		logSemanticGenerationSuperseded(ctx, intent, semanticFencePhasePostWrite)
		return semanticSupersededResult(intent, writeResult.CanonicalWrites), nil
	}
	return reducercontract.Result{}, newSemanticEntityIdentityDivergentError(writeResult)
}

// semanticSupersededResult acks an intent whose generation lost the freshness
// fence: superseded exits carry no failure, so the queue neither retries nor
// dead-letters work a newer generation already redid. canonicalWrites reports
// the pre-fence write outcome: zero on the pre-write fence (no write ran),
// the write result's count on the post-write re-check (rows already landed).
func semanticSupersededResult(intent reducercontract.Intent, canonicalWrites int) reducercontract.Result {
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainSemanticEntityMaterialization,
		Status:          reducercontract.ResultStatusSuperseded,
		EvidenceSummary: "semantic entity materialization skipped: generation superseded",
		CanonicalWrites: canonicalWrites,
	}
}

// semanticFencePhasePreWrite and semanticFencePhasePostWrite are the closed
// label set for the superseded-skip log's phase field: the fence that
// noticed the supersede ran before any graph write, or after the write on
// the unmatched-uid re-check.
const (
	semanticFencePhasePreWrite  = "pre_write"
	semanticFencePhasePostWrite = "post_write"
)

// logSemanticGenerationSuperseded emits one structured Info log per
// superseded skip so a fence exit stays queryable by scope, generation, and
// domain alongside the completion record it replaces. The phase field uses
// the literal "phase" key to match telemetry.MetricDimensionPhase without
// importing the telemetry package, which this family's import rule forbids.
func logSemanticGenerationSuperseded(ctx context.Context, intent reducercontract.Intent, phase string) {
	slog.InfoContext(
		ctx, "semantic entity materialization skipped: generation superseded",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(string(intent.Domain)),
		slog.String("phase", phase),
	)
}
