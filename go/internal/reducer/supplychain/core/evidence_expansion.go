// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// supplyChainImpactExpansionCounts is how many envelopes each expansion stage
// added to one pass's evidence, for the pass's diagnostic sub-signals.
type supplyChainImpactExpansionCounts struct {
	osPackageAdvisory        int
	osPackageAdvisorySkipped int
	scannerAnalysisScope     int
	resolvedDigest           int
}

// loadSupplyChainImpactExpansionEvidence runs the four expansion stages after
// the until-stable active-evidence load, in dependency order: OS-package
// advisory targets, the scanner-analysis fact of each scan scope those name,
// the container identities of the digests those resolve, and the peer
// identities of the repositories those cover. Each stage feeds the next.
//
// Every stage is an expansion stage. Once the per-intent evidence budget is
// spent the remaining ones are skipped and the pass is partial (#7154); a
// bounded suppression tail is recorded on truncation without stopping anything.
// timing receives the per-stage durations the handler reports.
func (h SupplyChainImpactHandler) loadSupplyChainImpactExpansionEvidence(
	ctx context.Context,
	envelopes []facts.Envelope,
	budget *supplyChainImpactEvidenceBudget,
	truncation *supplyChainImpactTruncation,
	timing *supplyChainImpactTiming,
) ([]facts.Envelope, supplyChainImpactExpansionCounts, error) {
	var counts supplyChainImpactExpansionCounts

	osPackageStart := len(envelopes)
	phaseStarted := time.Now()
	if !budget.exhausted() {
		loaded, skipped, err := h.loadSupplyChainImpactOSPackageAdvisoryFacts(ctx, envelopes, budget)
		if err != nil {
			timing.loadOSPackageAdvisoryDuration = time.Since(phaseStarted)
			return nil, counts, fmt.Errorf("load supply chain impact os package advisory facts: %w", err)
		}
		counts.osPackageAdvisorySkipped = skipped
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, loaded...)
	}
	timing.loadOSPackageAdvisoryDuration = time.Since(phaseStarted)
	counts.osPackageAdvisory = len(envelopes) - osPackageStart

	scannerStart := len(envelopes)
	var scannerEnvelopes []facts.Envelope
	phaseStarted = time.Now()
	if !budget.exhausted() {
		var err error
		scannerEnvelopes, err = h.loadSupplyChainImpactScannerAnalysisScopeFacts(ctx, envelopes, budget)
		if err != nil {
			timing.loadScannerAnalysisScopeDuration = time.Since(phaseStarted)
			return nil, counts, fmt.Errorf("load supply chain impact scanner analysis scope facts: %w", err)
		}
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, scannerEnvelopes...)
	}
	timing.loadScannerAnalysisScopeDuration = time.Since(phaseStarted)
	counts.scannerAnalysisScope = len(envelopes) - scannerStart

	resolvedStart := len(envelopes)
	var resolvedEnvelopes []facts.Envelope
	phaseStarted = time.Now()
	if !budget.exhausted() {
		var (
			tail bool
			err  error
		)
		resolvedEnvelopes, tail, err = h.loadSupplyChainImpactResolvedDigestEvidenceFacts(ctx, scannerEnvelopes, budget)
		if err != nil {
			timing.loadResolvedDigestEvidenceDuration = time.Since(phaseStarted)
			return nil, counts, fmt.Errorf("load supply chain impact resolved digest evidence facts: %w", err)
		}
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, resolvedEnvelopes...)
		truncation.suppressionTail = truncation.suppressionTail || tail
	}
	timing.loadResolvedDigestEvidenceDuration = time.Since(phaseStarted)
	counts.resolvedDigest = len(envelopes) - resolvedStart

	if !budget.exhausted() {
		peers, tail, err := h.loadSupplyChainImpactPeerIdentityFacts(ctx, resolvedEnvelopes, budget)
		if err != nil {
			return nil, counts, fmt.Errorf("load supply chain impact peer identity facts: %w", err)
		}
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, peers...)
		truncation.suppressionTail = truncation.suppressionTail || tail
	}
	truncation.budget = truncation.budget || budget.exhausted()
	return envelopes, counts, nil
}
