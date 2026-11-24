// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// GoReachabilityFactFilter bounds cross-scope govulncheck evidence loading
// for Go impact findings (eshu-hq/eshu#10095). RepositoryIDs are the
// canonical repository ids from the pass's filter; the storage reader maps
// each to its go-reachability scope's active generation and skips ids that
// cannot name one, so this package never imports the collector scope
// contract.
type GoReachabilityFactFilter struct {
	RepositoryIDs []string
}

// activeGoReachabilityFactLoader is the optional production seam behind the
// cross-scope Go stage. Loaders that do not implement it (including test
// doubles and every production store before the #10095 storage reader
// lands) leave the pass's verdicts exactly as they are today: the stage
// no-ops and findings without joined Go evidence read module_only.
type activeGoReachabilityFactLoader interface {
	ListActiveGoReachabilityFacts(context.Context, GoReachabilityFactFilter) ([]facts.Envelope, error)
}

// GoReachabilityEvidence is what one cross-scope Go evidence load returned: the
// envelopes and the (go scope, generation) pairs the loader resolved, read in the
// same snapshot (#11327). The pairs are what the writer locks and rechecks; a scope
// the filter named but the loader did not resolve is not in the set.
type GoReachabilityEvidence struct {
	Facts       []facts.Envelope
	Generations []GoEvidenceGeneration
}

// activeGoReachabilityEvidenceLoader is the production seam that also returns the
// resolved pairs. A loader that has only the fact-only seam above fails the pass
// closed: it cannot name a resolved go scope that holds no facts.
type activeGoReachabilityEvidenceLoader interface {
	ListActiveGoReachabilityEvidence(context.Context, GoReachabilityFactFilter) (GoReachabilityEvidence, error)
}

// loadGoReachabilityEvidenceFacts loads the active generation's
// vulnerability.go_module_evidence and vulnerability.go_call_reachability
// facts for every repository in the pass, from that repository's
// go-reachability scope. It is the only path Go evidence reaches a holder's
// pass through: supplyChainImpactFactKinds lists the two kinds for the
// intent's own scope, which never holds them in production.
func (h SupplyChainImpactHandler) loadGoReachabilityEvidenceFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	evidence, err := h.loadGoReachabilityEvidence(ctx, envelopes)
	return evidence.Facts, err
}

// loadGoReachabilityEvidence is loadGoReachabilityEvidenceFacts plus the resolved
// (go scope, generation) pairs the writer locks and rechecks (#11327).
func (h SupplyChainImpactHandler) loadGoReachabilityEvidence(
	ctx context.Context,
	envelopes []facts.Envelope,
) (GoReachabilityEvidence, error) {
	filter := goReachabilityFactFilter(envelopes)
	if len(filter.RepositoryIDs) == 0 {
		return GoReachabilityEvidence{}, nil
	}
	if loader, ok := h.FactLoader.(activeGoReachabilityEvidenceLoader); ok {
		evidence, err := loader.ListActiveGoReachabilityEvidence(ctx, filter)
		if err != nil {
			return GoReachabilityEvidence{}, factload.ClassifyFactLoadError(err)
		}
		return evidence, nil
	}
	if _, ok := h.FactLoader.(activeGoReachabilityFactLoader); ok {
		// A facts-only loader cannot say which go scopes it resolved, and a resolved scope
		// with no facts would be neither locked nor rechecked, so the pass fails closed
		// rather than guess the pairs from the envelopes (#11327). Production implements
		// the seam above.
		return GoReachabilityEvidence{}, fmt.Errorf("go reachability loader %T cannot report the go scopes it resolved: implement ListActiveGoReachabilityEvidence", h.FactLoader)
	}
	return GoReachabilityEvidence{}, nil
}

// appendGoReachabilityEvidenceFacts runs the #10095 cross-scope Go stage as
// one budget-checked step of loadSupplyChainImpactEvidence, right after the
// other reachability stages: it keys off the same repository ids they
// resolved, and it charges the evidence budget like the expansion stages, so
// a pass that spent its budget on advisory evidence skips this load and
// records truncation instead of silently under-reading govulncheck verdicts.
// It returns the extended evidence and how many envelopes the stage added.
func (h SupplyChainImpactHandler) appendGoReachabilityEvidenceFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
	truncation *supplyChainImpactTruncation,
	timing *supplyChainImpactTiming,
) ([]facts.Envelope, int, []GoEvidenceGeneration, error) {
	start := len(envelopes)
	started := time.Now()
	defer func() { timing.loadGoReachabilityDuration = time.Since(started) }()
	if budget.exhausted() {
		return envelopes, 0, nil, nil
	}
	loaded, err := h.loadGoReachabilityEvidence(ctx, envelopes)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("load Go reachability evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, loaded.Facts...)
	budget.charge(len(envelopes) - start)
	truncation.budget = truncation.budget || budget.exhausted()
	return envelopes, len(envelopes) - start, loaded.Generations, nil
}

// goReachabilityFactFilter keeps every repository id the pass already
// resolved, de-duplicated and sorted, minus the git-repository-scope alias
// form supplyChainImpactFilter adds for the branch probe: both name the same
// repository and the bare form is the probe key. The storage reader owns the
// canonical-id gate (GoReachabilityScopeID), so a repository the scanner
// never addresses (a repo:// or image scope id) still crosses this filter
// and is skipped there, not here.
func goReachabilityFactFilter(envelopes []facts.Envelope) GoReachabilityFactFilter {
	var ids []string
	for _, repositoryID := range supplyChainImpactFilter(envelopes).RepositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if repositoryID == "" || strings.HasPrefix(repositoryID, scope.GitRepositoryScopePrefix) {
			continue
		}
		ids = append(ids, repositoryID)
	}
	return GoReachabilityFactFilter{
		RepositoryIDs: payloadcore.UniqueSortedStrings(ids),
	}
}

// Proof record (benchmark figures, EXPLAIN, lane-1 halves) for #10095.
