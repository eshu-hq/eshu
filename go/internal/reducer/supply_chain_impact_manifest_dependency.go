// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
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
		ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(pkg.ecosystem))
		if ecosystem == "" {
			continue
		}
		ecosystems = append(ecosystems, string(ecosystem))
		packageIDs = append(packageIDs, pkg.packageID)
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

func supplyChainImpactManifestDependencyPackage(envelope facts.Envelope) (supplyChainAffectedPackage, bool) {
	switch envelope.FactKind {
	case facts.VulnerabilityAffectedPackageFactKind:
		return supplyChainAffectedPackageFromEnvelope(envelope), true
	case facts.SecurityAlertRepositoryAlertFactKind:
		return supplyChainAffectedPackageFromSecurityAlert(providerSecurityAlert{
			SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
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
		return supplyChainAffectedPackage{}, false
	}
}

func supplyChainAffectedPackageNameCandidates(pkg supplyChainAffectedPackage) []string {
	candidates := []string{pkg.name}
	candidates = append(candidates, packageNameFromPURL(pkg.purl))
	candidates = append(candidates, packageNameFromPackageID(pkg.packageID))
	return uniqueSortedStrings(candidates)
}

func packageNameFromPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	beforeQuery, _, _ := strings.Cut(raw, "?")
	_, path, ok := strings.Cut(beforeQuery, "/")
	if !ok {
		return ""
	}
	if versionAt := strings.LastIndex(path, "@"); versionAt > 0 {
		path = path[:versionAt]
	}
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return strings.TrimSpace(path)
	}
	return strings.TrimSpace(decoded)
}

func packageNameFromPackageID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	_, afterScheme, ok := strings.Cut(raw, "://")
	if !ok {
		return ""
	}
	_, path, ok := strings.Cut(afterScheme, "/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(path)
}
