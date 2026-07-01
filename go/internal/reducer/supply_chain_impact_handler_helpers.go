// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func (h SupplyChainImpactHandler) evaluationNow() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeSupplyChainImpactFactLoader)
	if !ok || filter.empty() {
		return nil, nil
	}
	envelopes, err := loader.ListActiveSupplyChainImpactFacts(ctx, filter)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

const maxSupplyChainImpactActiveEvidenceLoads = 8

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactFactsUntilStable(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, bool, error) {
	requested := SupplyChainImpactFactFilter{}
	next := supplyChainImpactFilter(envelopes)
	for loads := 0; !next.empty(); loads++ {
		if loads >= maxSupplyChainImpactActiveEvidenceLoads {
			return envelopes, true, nil
		}
		active, err := h.loadActiveSupplyChainImpactFacts(ctx, next)
		if err != nil {
			return nil, false, err
		}
		requested = mergeSupplyChainImpactFactFilters(requested, next)
		envelopes = appendUniqueSupplyChainImpactFacts(envelopes, active...)
		next = supplyChainImpactFollowUpFilter(requested, supplyChainImpactFilter(envelopes))
	}
	return envelopes, false, nil
}

func (h SupplyChainImpactHandler) emitCounters(
	ctx context.Context,
	counts map[SupplyChainImpactStatus]int,
	suppressionCounts map[SupplyChainSuppressionState]int,
	remediationCounts map[supplyChainRemediationKey]int,
) {
	if h.Instruments == nil {
		return
	}
	for _, status := range supplyChainImpactStatuses() {
		if counts[status] == 0 {
			continue
		}
		h.Instruments.SupplyChainImpactFindings.Add(ctx, int64(counts[status]), metric.WithAttributes(
			telemetry.AttrDomain(string(DomainSupplyChainImpact)),
			telemetry.AttrOutcome(string(status)),
		))
	}
	if h.Instruments.SupplyChainSuppressionDecisions != nil {
		for _, state := range SupplyChainSuppressionStates() {
			if suppressionCounts[state] == 0 {
				continue
			}
			h.Instruments.SupplyChainSuppressionDecisions.Add(ctx, int64(suppressionCounts[state]), metric.WithAttributes(
				telemetry.AttrDomain(string(DomainSupplyChainImpact)),
				telemetry.AttrOutcome(string(state)),
			))
		}
	}
	if h.Instruments.SupplyChainRemediationDecisions != nil {
		for key, count := range remediationCounts {
			if count == 0 {
				continue
			}
			h.Instruments.SupplyChainRemediationDecisions.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrDomain(string(DomainSupplyChainImpact)),
				telemetry.AttrOutcome(key.confidence),
				telemetry.AttrReason(key.reason),
			))
		}
	}
}

// supplyChainRemediationKey bounds the remediation counter cardinality to
// the closed product of (confidence, reason) labels.
type supplyChainRemediationKey struct {
	confidence string
	reason     string
}

func supplyChainRemediationCounts(findings []SupplyChainImpactFinding) map[supplyChainRemediationKey]int {
	out := make(map[supplyChainRemediationKey]int)
	for _, finding := range findings {
		confidence := strings.TrimSpace(finding.Remediation.Confidence)
		reason := strings.TrimSpace(finding.Remediation.Reason)
		if confidence == "" && reason == "" {
			continue
		}
		if confidence == "" {
			confidence = SupplyChainRemediationConfidenceUnknown
		}
		out[supplyChainRemediationKey{confidence: confidence, reason: reason}]++
	}
	return out
}
