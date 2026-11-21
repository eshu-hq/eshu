// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// loadActiveSupplyChainImpactFactsUntilStable takes its FIRST-round filter from
// the caller rather than deriving it again from envelopes. The #5709 readiness
// floor decides whether this pass can reach a producer fact by inspecting that
// same filter before the load runs (crossScopeProducerLookupPlanned). Sharing
// one value keeps the floor and the load from disagreeing about what this pass
// asked for, and keeps the pre-load envelope set scanned once rather than twice
// on a hot path. Follow-up rounds are still derived here from whatever the
// previous round returned.
//
// The returned truncation names why the loop stopped short: the round cap
// (rounds), a spent evidence budget (budget), or a bounded suppression tail
// (suppressionTail) -- the causes stay apart because only the first two make
// the finding set incomplete (#7154).
// loadActiveSupplyChainImpactFactsUntilStable is the until-stable loop with
// an optional #9779 scope: when set, every follow-up round intersects its
// CVE- and package-bearing arms with the scope, so the loop converges on the
// stamped subgraph instead of re-widening to the whole scope. Arms without
// CVE or package linkage flow through, bounded by discovery like the full
// pass. A follow-up the scope empties ends the loop: nothing reachable
// remains unrequested.
//
// ownScopeID is the intent scope: under the #11087 bound only components of
// that scope bear component-origin criteria (criteria_bound.go).
func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFactsUntilStable(
	ctx context.Context,
	envelopes []facts.Envelope,
	initialFilter SupplyChainImpactFactFilter,
	budget *supplyChainImpactEvidenceBudget,
	scoped *scopedSupplyChainImpactScope,
	ownScopeID string,
) ([]facts.Envelope, supplyChainImpactTruncation, supplyChainImpactCriteriaBoundOutcome, error) {
	requested := SupplyChainImpactFactFilter{}
	next := initialFilter
	var truncation supplyChainImpactTruncation
	// The #11087 provenance bound is loop-local state (criteria_bound.go):
	// nil keeps the loop byte-for-byte on the prior semantics.
	var bound *criteriaExpansionBoundState
	if h.CriteriaExpansionBound != nil {
		bound = newCriteriaExpansionBoundState(initialFilter, scoped, ownScopeID)
		next, _ = bound.apply(next, envelopes, envelopes, requested)
	}
	// Parser file envelopes are folded, not kept (#9781): the one digest
	// envelope goes where the files would have gone, after the pass's earlier
	// files and before any file a later stage loads.
	fold := NewFileReachabilityFold(envelopes)
	withDigest := func(out []facts.Envelope) []facts.Envelope {
		if digest, ok := fold.DigestEnvelope(); ok {
			return append(out, digest)
		}
		return out
	}
	for loads := 0; !next.empty(); loads++ {
		if loads >= maxSupplyChainImpactActiveEvidenceLoads {
			truncation.rounds = true
			return withDigest(envelopes), truncation, bound.outcome(), nil
		}
		next.FileFold = fold
		if bound != nil {
			bound.loads = loads + 1
		}
		active, roundSuppressionTail, err := h.loadActiveSupplyChainImpactFacts(ctx, next, budget)
		if err != nil {
			return nil, supplyChainImpactTruncation{}, bound.outcome(), err
		}
		// A single round's own per-call suppression cap is independent of the
		// round cap above (#5466 round-7 review P1-B).
		truncation.suppressionTail = truncation.suppressionTail || roundSuppressionTail
		requested = mergeSupplyChainImpactFactFilters(requested, next)
		before := len(envelopes)
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, active...)
		if !budget.charge(len(envelopes) - before) {
			// Keep what this round loaded, load nothing more.
			truncation.budget = true
			return withDigest(envelopes), truncation, bound.outcome(), nil
		}
		next = supplyChainImpactFollowUpFilter(requested, supplyChainImpactFilter(envelopes))
		// The provenance bound applies before the scoped restrict so the
		// restrict still sees the full derived arms.
		next, _ = bound.apply(next, envelopes, active, requested)
		if scoped != nil {
			next = restrictSupplyChainImpactFilter(next, scoped)
		}
	}
	return withDigest(envelopes), truncation, bound.outcome(), nil
}
