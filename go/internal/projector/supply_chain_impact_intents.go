// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// supplyChainImpactCandidateFactKinds are the fact kinds
// supplyChainImpactTriggerFact accepts.
var supplyChainImpactCandidateFactKinds = []string{
	facts.VulnerabilityCVEFactKind,
	facts.VulnerabilityAffectedPackageFactKind,
	facts.VulnerabilityEPSSScoreFactKind,
	facts.VulnerabilityKnownExploitedFactKind,
	facts.SecurityAlertRepositoryAlertFactKind,
	facts.PackageRegistryPackageFactKind,
	facts.SBOMComponentFactKind,
	facts.OCIImageManifestFactKind,
	facts.OCIImageIndexFactKind,
	facts.OCIImageTagObservationFactKind,
	facts.OCIImageReferrerFactKind,
}

func buildSupplyChainImpactReducerIntent(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	index *reducerIntentFactIndex,
) (ReducerIntent, bool) {
	envelope, ok := index.firstAcrossKinds(supplyChainImpactTriggerFact, supplyChainImpactCandidateFactKinds...)
	if !ok {
		return ReducerIntent{}, false
	}
	return ReducerIntent{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Domain:       reducer.DomainSupplyChainImpact,
		EntityKey:    "supply_chain_impact:" + scopeValue.ScopeID,
		Reason:       supplyChainImpactReason(envelope),
		FactID:       envelope.FactID,
		SourceSystem: supplyChainImpactSourceSystem(envelope),
	}, true
}

func supplyChainImpactTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.VulnerabilityCVEFactKind,
		facts.VulnerabilityAffectedPackageFactKind,
		facts.VulnerabilityEPSSScoreFactKind,
		facts.VulnerabilityKnownExploitedFactKind,
		facts.SecurityAlertRepositoryAlertFactKind,
		facts.PackageRegistryPackageFactKind,
		facts.SBOMComponentFactKind,
		facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageReferrerFactKind:
		return true
	default:
		return false
	}
}

func supplyChainImpactReason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.SecurityAlertRepositoryAlertFactKind {
		return "provider security alert evidence observed"
	}
	if envelope.FactKind == facts.PackageRegistryPackageFactKind {
		return "package registry identity observed"
	}
	if envelope.FactKind == facts.SBOMComponentFactKind {
		return "SBOM package evidence observed"
	}
	switch envelope.FactKind {
	case facts.OCIImageManifestFactKind,
		facts.OCIImageIndexFactKind,
		facts.OCIImageTagObservationFactKind,
		facts.OCIImageReferrerFactKind:
		return "OCI image subject evidence observed"
	}
	return "supply-chain vulnerability evidence observed"
}

func supplyChainImpactSourceSystem(envelope facts.Envelope) string {
	if value := strings.TrimSpace(envelope.SourceRef.SourceSystem); value != "" {
		return value
	}
	return strings.TrimSpace(envelope.CollectorKind)
}
