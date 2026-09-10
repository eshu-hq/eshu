// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func (h SupplyChainImpactHandler) loadActivePackageManifestDependencyFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(correlation.ActivePackageManifestDependencyFactLoader)
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
		return nil, factload.ClassifyFactLoadError(err)
	}
	return dependencies, nil
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
