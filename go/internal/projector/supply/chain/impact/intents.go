// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// candidateFactKinds are the fact kinds triggerFact ever returns true for.
var candidateFactKinds = []string{
	chain.VulnerabilityCVEFactKind,
	chain.VulnerabilityAffectedPackageFactKind,
	chain.VulnerabilityEPSSScoreFactKind,
	chain.VulnerabilityKnownExploitedFactKind,
	chain.VulnerabilitySuppressionFactKind,
	facts.SecurityAlertRepositoryAlertFactKind,
	chain.PackageRegistryPackageFactKind,
	chain.SBOMComponentFactKind,
	chain.OCIImageManifestFactKind,
	chain.OCIImageIndexFactKind,
	chain.OCIImageTagObservationFactKind,
	chain.OCIImageReferrerFactKind,
}

// BuildSupplyChainImpactReducerIntent enqueues one supply_chain_impact
// reducer intent per scope generation that observed vulnerability
// intelligence, a provider security alert, package-registry identity, an
// SBOM component, or OCI manifest/index/tag/referrer evidence. Any other git
// generation gets one manifest marker intent instead (#9874, see
// manifest_trigger.go). The reducer
// owns the cross-source vulnerability-to-package-to-deployment join; this
// package only selects the trigger fact, a short human-readable reason, and
// the source-system label.
func BuildSupplyChainImpactReducerIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	// #10077: a go-reachability:// generation raises its own pass so a
	// successor (or a trusted no_go_modules retraction) re-derives the
	// findings through ChangedCVEIDs fan-out. The trigger is the scope id
	// form, not the fact kinds alone, so a warning-only generation also
	// raises a pass. Anything unanchored falls through to the shared path.
	if _, ok := scannercomponent.ParseGoReachabilityScopeID(scopeID); ok {
		if intent, ok := buildGoReachabilityIntent(scopeID, generationID, lookup); ok {
			return intent, true
		}
	}
	envelope, ok := lookup.FirstAcrossKinds(triggerFact, candidateFactKinds...)
	if !ok {
		return buildManifestDependencyIntent(scopeID, generationID, lookup)
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
	case chain.VulnerabilityCVEFactKind,
		chain.VulnerabilityAffectedPackageFactKind,
		chain.VulnerabilityEPSSScoreFactKind,
		chain.VulnerabilityKnownExploitedFactKind,
		chain.VulnerabilitySuppressionFactKind,
		facts.SecurityAlertRepositoryAlertFactKind,
		chain.PackageRegistryPackageFactKind,
		chain.SBOMComponentFactKind,
		chain.OCIImageManifestFactKind,
		chain.OCIImageIndexFactKind,
		chain.OCIImageTagObservationFactKind,
		chain.OCIImageReferrerFactKind:
		return true
	default:
		return false
	}
}

// goReachabilityFactKinds are the fact kinds a go-reachability://
// generation anchors its supply_chain_impact pass on: the two govulncheck
// evidence kinds for a successor, and vulnerability.warning so a trusted
// no_go_modules generation (warning only, no Go facts) raises the pass that
// retracts the previous generation's copies (#10077). The retraction kind
// is vulnerability.warning: the planner's retraction fixture
// (TestListGoModuleRepositoriesDropsRepositoryAfterRetractionLive) seeds the
// post-retraction active generation holding exactly that kind.
// scanner_worker.warning is deliberately absent: it models transient scan
// failures (analysis_failed, toolchain_unsupported) and a failed analysis
// must not retract a prior run's evidence, so that kind never anchors a
// pass whatever payload it carries.
var goReachabilityFactKinds = []string{
	chain.VulnerabilityGoModuleEvidenceFactKind,
	chain.VulnerabilityGoCallReachabilityFactKind,
	chain.VulnerabilityWarningFactKind,
}

// goReachabilityReason is the human-readable reason of a Go-evidence
// go-reachability:// pass.
const goReachabilityReason = "go reachability evidence observed"

// goReachabilityWarningReason is the human-readable reason of a
// warning-anchored go-reachability:// pass. It differs from the evidence
// reason on purpose: a warning-only pass observed no evidence, and the
// reason is how operators tell a retraction from a finding-bearing pass.
const goReachabilityWarningReason = "go reachability warning observed"

// buildGoReachabilityIntent returns the supply_chain_impact intent for a
// go-reachability:// generation anchored on its earliest Go evidence or
// warning fact, or false when the generation holds neither. The entity key
// matches the shared supply_chain_impact:<scope> form so the pass writes
// its copies under the Go generation and the writer reports their CVE ids
// for holder fan-out.
func buildGoReachabilityIntent(
	scopeID string,
	generationID string,
	lookup projectorintent.FactLookup,
) (projectorintent.ReducerIntent, bool) {
	envelope, ok := lookup.FirstAcrossKinds(goReachabilityTriggerFact, goReachabilityFactKinds...)
	if !ok {
		return projectorintent.ReducerIntent{}, false
	}
	reason := goReachabilityReason
	if envelope.FactKind == chain.VulnerabilityWarningFactKind {
		reason = goReachabilityWarningReason
	}
	return projectorintent.ReducerIntent{
		ScopeID:      scopeID,
		GenerationID: generationID,
		Domain:       reducer.DomainSupplyChainImpact,
		EntityKey:    "supply_chain_impact:" + scopeID,
		Reason:       reason,
		FactID:       envelope.FactID,
		SourceSystem: projectorintent.SourceSystem(envelope),
	}, true
}

// goReachabilityTriggerFact reports whether envelope anchors a
// go-reachability:// pass. The kind list already restricts the lookup to
// the three anchor kinds; the predicate keeps the FirstAcrossKinds shape
// the other triggers use.
func goReachabilityTriggerFact(envelope facts.Envelope) bool {
	switch envelope.FactKind {
	case chain.VulnerabilityGoModuleEvidenceFactKind,
		chain.VulnerabilityGoCallReachabilityFactKind,
		chain.VulnerabilityWarningFactKind:
		return true
	default:
		return false
	}
}

func reason(envelope facts.Envelope) string {
	if envelope.FactKind == facts.SecurityAlertRepositoryAlertFactKind {
		return "provider security alert evidence observed"
	}
	if envelope.FactKind == chain.PackageRegistryPackageFactKind {
		return "package registry identity observed"
	}
	if envelope.FactKind == chain.SBOMComponentFactKind {
		return "SBOM package evidence observed"
	}
	if envelope.FactKind == chain.VulnerabilitySuppressionFactKind {
		return "vulnerability suppression evidence observed"
	}
	switch envelope.FactKind {
	case chain.OCIImageManifestFactKind,
		chain.OCIImageIndexFactKind,
		chain.OCIImageTagObservationFactKind,
		chain.OCIImageReferrerFactKind:
		return "OCI image subject evidence observed"
	}
	return "supply-chain vulnerability evidence observed"
}
