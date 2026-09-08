// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func (h SupplyChainImpactHandler) loadActivePackageManifestDependencyFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activePackageManifestDependencyFactLoader)
	if !ok {
		return nil, nil
	}
	filter := supplyChainImpactManifestDependencyFilter(envelopes)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, nil
	}
	dependencies, err := loader.ListActivePackageManifestDependencyFacts(
		ctx,
		filter.Ecosystems,
		filter.PackageNames,
	)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return dependencies, nil
}

func supplyChainImpactManifestDependencyFilter(envelopes []facts.Envelope) PackageManifestDependencyFactFilter {
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
			names = append(names, packageConsumptionNameCandidates(ecosystem, name)...)
		}
	}
	return PackageManifestDependencyFactFilter{
		Ecosystems:   uniqueSortedStrings(ecosystems),
		PackageNames: uniqueSortedStrings(names),
		PackageIDs:   uniqueSortedStrings(packageIDs),
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
	case facts.VulnerabilityAffectedPackageFactKind:
		pkg, err := supplyChainAffectedPackageFromEnvelope(envelope)
		if err != nil {
			return supplychainmodel.AffectedPackage{}, false
		}
		return pkg, true
	case facts.SecurityAlertRepositoryAlertFactKind:
		return supplyChainAffectedPackageFromSecurityAlert(securityalert.ProviderSecurityAlert{
			SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
				ProviderAlertFactID: envelope.FactID,
				Provider:            payloadStr(envelope.Payload, "provider"),
				ProviderAlertID:     payloadStr(envelope.Payload, "provider_alert_id"),
				ProviderState:       strings.ToLower(payloadStr(envelope.Payload, "provider_state")),
				RepositoryID:        payloadStr(envelope.Payload, "repository_id"),
				PackageID:           payloadStr(envelope.Payload, "package_id"),
				Ecosystem:           payloadStr(envelope.Payload, "ecosystem"),
				PackageName:         payloadStr(envelope.Payload, "package_name"),
				GHSAIDs:             payloadStrings(envelope.Payload, "ghsa_id", "ghsa_ids"),
				CVEIDs:              payloadStrings(envelope.Payload, "cve_id", "cve_ids"),
				VulnerableRange:     payloadStr(envelope.Payload, "vulnerable_range"),
				PatchedVersion:      payloadStr(envelope.Payload, "patched_version"),
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
	return uniqueSortedStrings(candidates)
}

// packageNameFromPURL forwards to [payloadcore.PackageNameFromPURL].
func packageNameFromPURL(raw string) string {
	return payloadcore.PackageNameFromPURL(raw)
}

// packageNameFromPackageID forwards to [payloadcore.PackageNameFromPackageID].
func packageNameFromPackageID(raw string) string {
	return payloadcore.PackageNameFromPackageID(raw)
}
