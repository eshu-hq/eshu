// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainimpact

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// candidateFactKinds are the fact kinds triggerFact ever returns true for.
var candidateFactKinds = []string{
	facts.VulnerabilityCVEFactKind,
	facts.VulnerabilityAffectedPackageFactKind,
	facts.VulnerabilityEPSSScoreFactKind,
	facts.VulnerabilityKnownExploitedFactKind,
	facts.VulnerabilitySuppressionFactKind,
	facts.SecurityAlertRepositoryAlertFactKind,
	facts.PackageRegistryPackageFactKind,
	facts.SBOMComponentFactKind,
	facts.OCIImageManifestFactKind,
	facts.OCIImageIndexFactKind,
	facts.OCIImageTagObservationFactKind,
	facts.OCIImageReferrerFactKind,
}

// BuildSupplyChainImpactReducerIntent enqueues one supply_chain_impact
// reducer intent per scope generation that observed vulnerability
// intelligence, a provider security alert, package-registry identity, an
// SBOM component, or OCI manifest/index/tag/referrer evidence. The reducer
// owns the cross-source vulnerability-to-package-to-deployment join; this
// package only selects the trigger fact, a short human-readable reason, and
// the source-system label.
func BuildSupplyChainImpactReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(triggerFact, candidateFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSupplyChainImpact,
		EntityKey:    "supply_chain_impact:" + scopeID,
		Reason:       reason(envelope),
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

func triggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case facts.VulnerabilityCVEFactKind,
		facts.VulnerabilityAffectedPackageFactKind,
		facts.VulnerabilityEPSSScoreFactKind,
		facts.VulnerabilityKnownExploitedFactKind,
		facts.VulnerabilitySuppressionFactKind,
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

func reason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.SecurityAlertRepositoryAlertFactKind {
		return "provider security alert evidence observed"
	}
	if envelope.FactKind == facts.PackageRegistryPackageFactKind {
		return "package registry identity observed"
	}
	if envelope.FactKind == facts.SBOMComponentFactKind {
		return "SBOM package evidence observed"
	}
	if envelope.FactKind == facts.VulnerabilitySuppressionFactKind {
		return "vulnerability suppression evidence observed"
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
