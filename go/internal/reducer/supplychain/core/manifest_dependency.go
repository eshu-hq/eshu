// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// loadActivePackageManifestDependencyFacts loads the manifest dependencies
// one pass matches against, and whether the read refused a reached
// delta-active scope's base (#10109). This handler retracts superseded
// findings before it writes, so it reads refusal-aware: a refusal is
// incomplete evidence, not a failed load — the caller marks the pass partial
// (upsert only, retract nothing) and proceeds with no manifest rows. A
// retryable probe failure stays an error so the queue retries it. A loader
// without the seam is a forgotten wire, not an empty manifest set: fail
// closed.
func (h SupplyChainImpactHandler) loadActivePackageManifestDependencyFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, bool, error) {
	filter := supplyChainImpactManifestDependencyFilter(envelopes)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, false, nil
	}
	loader, ok := h.FactLoader.(correlation.ManifestDependencyRefusingLoader)
	if !ok {
		return nil, false, fmt.Errorf("supply chain impact manifest dependency refusing loader is required")
	}
	dependencies, refused, err := loader.ListActivePackageManifestDependencyFactsWithRefusals(
		ctx,
		filter.Ecosystems,
		filter.PackageNames,
	)
	if refused {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, factload.ClassifyFactLoadError(err)
	}
	return dependencies, false, nil
}

// loadScopedPackageManifestDependencyFacts is the #9779 scoped manifest
// stage: the name list derives from the stamped envelopes, unioned with the
// finding-row seeds whose advisories may live cross-scope. Unscoped (nil
// scope) it is the plain stage.
func (h SupplyChainImpactHandler) loadScopedPackageManifestDependencyFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
	filterEnvelopes []facts.Envelope,
	scoped *scopedSupplyChainImpactScope,
) ([]facts.Envelope, bool, error) {
	if scoped == nil {
		return h.loadActivePackageManifestDependencyFacts(ctx, envelopes)
	}
	filter := restrictManifestFilterToScope(supplyChainImpactManifestDependencyFilter(filterEnvelopes), scoped)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, false, nil
	}
	loader, ok := h.FactLoader.(correlation.ManifestDependencyRefusingLoader)
	if !ok {
		return nil, false, fmt.Errorf("supply chain impact manifest dependency refusing loader is required")
	}
	dependencies, refused, err := loader.ListActivePackageManifestDependencyFactsWithRefusals(
		ctx,
		filter.Ecosystems,
		filter.PackageNames,
	)
	if refused {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, factload.ClassifyFactLoadError(err)
	}
	return dependencies, false, nil
}

func supplyChainImpactManifestDependencyFilter(envelopes []facts.Envelope) correlation.PackageManifestDependencyFactFilter {
	var ecosystems []string
	var names []string
	var packageIDs []string
	for _, envelope := range envelopes {
		if envelope.IsTombstone {
			continue
		}
		pkg, ok := supplyChainImpactManifestDependencyPackage(envelope)
		if !ok {
			continue
		}
		ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.Ecosystem))
		if ecosystem == "" {
			continue
		}
		ecosystems = append(ecosystems, string(ecosystem))
		packageIDs = append(packageIDs, pkg.PackageID)
		for _, name := range supplyChainAffectedPackageNameCandidates(pkg) {
			names = append(names, correlation.PackageConsumptionNameCandidates(ecosystem, name)...)
		}
	}
	return correlation.PackageManifestDependencyFactFilter{
		Ecosystems:   payloadcore.UniqueSortedStrings(ecosystems),
		PackageNames: payloadcore.UniqueSortedStrings(names),
		PackageIDs:   payloadcore.UniqueSortedStrings(packageIDs),
	}
}

// supplyChainImpactManifestDependencyPackage is a best-effort filter-hint
// helper (feeding the follow-up manifest-dependency query filter), not the
// authoritative decode path — buildSupplyChainImpactIndex is. A
// vulnerability.affected_package fact that fails typed decode here reports
// "not usable as a filter hint" (ok=false) rather than quarantining; the
// authoritative index build still quarantines and reports it as an
// input_invalid dead-letter.
func supplyChainImpactManifestDependencyPackage(envelope facts.Envelope) (supplychainmodel.AffectedPackage, bool) {
	switch envelope.FactKind {
	case chain.VulnerabilityAffectedPackageFactKind:
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			return supplychainmodel.AffectedPackage{}, false
		}
		return pkg, true
	case facts.SecurityAlertRepositoryAlertFactKind:
		return supplyChainAffectedPackageFromSecurityAlert(securityalert.ProviderSecurityAlert{
			SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
				ProviderAlertFactID: envelope.FactID,
				Provider:            payloadcore.PayloadStr(envelope.Payload, "provider"),
				ProviderAlertID:     payloadcore.PayloadStr(envelope.Payload, "provider_alert_id"),
				ProviderState:       strings.ToLower(payloadcore.PayloadStr(envelope.Payload, "provider_state")),
				RepositoryID:        payloadcore.PayloadStr(envelope.Payload, "repository_id"),
				PackageID:           payloadcore.PayloadStr(envelope.Payload, "package_id"),
				Ecosystem:           payloadcore.PayloadStr(envelope.Payload, "ecosystem"),
				PackageName:         payloadcore.PayloadStr(envelope.Payload, "package_name"),
				GHSAIDs:             payloadcore.PayloadStrings(envelope.Payload, "ghsa_id", "ghsa_ids"),
				CVEIDs:              payloadcore.PayloadStrings(envelope.Payload, "cve_id", "cve_ids"),
				VulnerableRange:     payloadcore.PayloadStr(envelope.Payload, "vulnerable_range"),
				PatchedVersion:      payloadcore.PayloadStr(envelope.Payload, "patched_version"),
			},
		}), true
	default:
		return supplychainmodel.AffectedPackage{}, false
	}
}

func supplyChainAffectedPackageNameCandidates(pkg supplychainmodel.AffectedPackage) []string {
	candidates := []string{pkg.Name}
	candidates = append(candidates, packageNameFromPURL(pkg.PURL))
	candidates = append(candidates, packageNameFromPackageID(pkg.PackageID))
	return payloadcore.UniqueSortedStrings(candidates)
}

// packageNameFromPURL forwards to [payloadcore.PackageNameFromPURL].
func packageNameFromPURL(raw string) string {
	return payloadcore.PackageNameFromPURL(raw)
}

// packageNameFromPackageID forwards to [payloadcore.PackageNameFromPackageID].
func packageNameFromPackageID(raw string) string {
	return payloadcore.PackageNameFromPackageID(raw)
}
