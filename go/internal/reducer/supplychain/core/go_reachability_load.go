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
	loader, ok := h.FactLoader.(activeGoReachabilityFactLoader)
	if !ok {
		return nil, nil
	}
	filter := goReachabilityFactFilter(envelopes)
	if len(filter.RepositoryIDs) == 0 {
		return nil, nil
	}
	reachabilityFacts, err := loader.ListActiveGoReachabilityFacts(ctx, filter)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return reachabilityFacts, nil
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
) ([]facts.Envelope, int, error) {
	start := len(envelopes)
	started := time.Now()
	defer func() { timing.loadGoReachabilityDuration = time.Since(started) }()
	if budget.exhausted() {
		return envelopes, 0, nil
	}
	loaded, err := h.loadGoReachabilityEvidenceFacts(ctx, envelopes)
	if err != nil {
		return nil, 0, fmt.Errorf("load Go reachability evidence facts: %w", err)
	}
	envelopes = appendUniqueSupplyChainImpactFacts(envelopes, loaded...)
	budget.charge(len(envelopes) - start)
	truncation.budget = truncation.budget || budget.exhausted()
	return envelopes, len(envelopes) - start, nil
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

// Proof record (benchmark figures, EXPLAIN, lane-1 halves):
// docs/internal/evidence/10095-cross-scope-go-loader.md.
