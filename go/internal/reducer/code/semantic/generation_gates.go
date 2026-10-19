// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// This file holds the handler's generation gates: the prior-generation
// retract decision and the #8611 freshness fence. Both answer "should this
// generation's run proceed" from generation state, never from row content,
// so they live together while Handle keeps only the two fence call sites.

package semantic

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// ErrGenerationSuperseded is the sentinel a writer-side generation fence
// aborts with when the owning generation loses freshness mid-write (#8628).
// The abort carries a partial EntityWriteResult whose CanonicalWrites counts
// dispatched rows only, so the handler can ack the superseded run with proof
// of what landed. It unwraps with errors.Is; a fence lookup error (including
// GenerationNotYetActiveError) never maps to it and propagates wrapped.
var ErrGenerationSuperseded = errors.New("semantic entity write superseded by a newer generation")

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

// semanticEntityWriteRequest stamps one canonical write with the intent's
// owning generation so the writer-side fence (#8628) can abort a superseded
// run between groups.
func semanticEntityWriteRequest(
	intent reducercontract.Intent,
	repoIDs []string,
	rows []EntityRow,
	skipRetract bool,
	delta semanticDeltaProjectionScope,
) EntityWrite {
	return EntityWrite{
		RepoIDs:         repoIDs,
		Rows:            rows,
		SkipRetract:     skipRetract,
		DeltaProjection: delta.Delta,
		DeltaFilePaths:  delta.FilePaths,
		ScopeID:         intent.ScopeID,
		GenerationID:    intent.GenerationID,
	}
}

// dispatchSemanticEntityWrite runs the graph write and maps a writer-side
// fence abort (#8628) to the mid-write superseded result: the partial
// CanonicalWrites rides the result, the mid_write superseded log emits, and
// the run acks with no phase publish, no poison-pill failure, and no retry.
// Any other write error propagates wrapped.
func (h Handler) dispatchSemanticEntityWrite(
	ctx context.Context,
	intent reducercontract.Intent,
	write EntityWrite,
) (writeResult EntityWriteResult, outcome reducercontract.Result, fenced bool, err error) {
	writeResult, err = h.Writer.WriteSemanticEntities(ctx, write)
	if err != nil {
		if errors.Is(err, ErrGenerationSuperseded) {
			logSemanticGenerationSuperseded(ctx, intent, semanticFencePhaseMidWrite)
			return writeResult, semanticSupersededResult(intent, writeResult.CanonicalWrites), true, nil
		}
		return EntityWriteResult{}, reducercontract.Result{}, false, fmt.Errorf("write semantic entities: %w", err)
	}
	return writeResult, reducercontract.Result{}, false, nil
}

// resolveSemanticPostWriteOutcome runs the single post-write freshness probe
// (#8628) after the completion log and feeds both branches: a stale
// generation exits superseded carrying the write's CanonicalWrites whether
// or not any uid went unmatched (unmatched uids on a superseded generation
// are nodes the newer generation legitimately removed, not a derivation
// defect, #8611); a still-current generation with unmatched uids keeps the
// #8587 poison-pill failure; a still-current generation with every row
// matched succeeds. A fence error propagates fail-closed.
func (h Handler) resolveSemanticPostWriteOutcome(
	ctx context.Context,
	intent reducercontract.Intent,
	writeResult EntityWriteResult,
	unmatched bool,
	summary string,
) (reducercontract.Result, error) {
	fresh, err := h.checkSemanticGenerationFreshness(ctx, intent)
	if err != nil {
		return reducercontract.Result{}, err
	}
	if !fresh {
		logSemanticGenerationSuperseded(ctx, intent, semanticFencePhasePostWrite)
		return semanticSupersededResult(intent, writeResult.CanonicalWrites), nil
	}
	if unmatched {
		return reducercontract.Result{}, newSemanticEntityIdentityDivergentError(writeResult)
	}
	return reducercontract.Result{
		IntentID:        intent.IntentID,
		Domain:          reducercontract.DomainSemanticEntityMaterialization,
		Status:          reducercontract.ResultStatusSucceeded,
		EvidenceSummary: summary,
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
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

// semanticFencePhasePreWrite, semanticFencePhaseMidWrite, and
// semanticFencePhasePostWrite are the closed label set for the
// superseded-skip log's phase field: the fence that noticed the supersede
// ran before any graph write, between writer groups on a fenced abort, or
// after the write on the single post-write probe.
const (
	semanticFencePhasePreWrite  = "pre_write"
	semanticFencePhaseMidWrite  = "mid_write"
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
