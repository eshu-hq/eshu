// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

const (
	securityAlertInstalledVersionMissing   = "installed package version missing"
	securityAlertInstalledVersionMalformed = "installed package version malformed"
)

func applySecurityAlertDependencyEvidence(
	decision *SecurityAlertReconciliationDecision,
	alert ProviderSecurityAlert,
	consumption SecurityAlertConsumption,
) {
	decision.DependencyEvidenceID = consumption.FactID
	decision.DependencyEvidenceKind = securityAlertConsumptionEvidenceKind(consumption)
	decision.DependencyRange = strings.TrimSpace(consumption.DependencyRange)
	decision.RequestedRange = payloadcore.FirstNonBlank(
		strings.TrimSpace(consumption.RequestedRange),
		decision.DependencyRange,
	)
	decision.ObservedVersion = securityAlertObservedVersion(alert, consumption)
	decision.PackageMissingEvidence = payloadcore.UniqueSortedStrings(append(
		decision.PackageMissingEvidence,
		securityAlertObservedVersionMissingEvidence(consumption, decision.ObservedVersion)...,
	))
}

func securityAlertObservedVersion(
	alert ProviderSecurityAlert,
	consumption SecurityAlertConsumption,
) string {
	if observedVersion := strings.TrimSpace(consumption.ObservedVersion); observedVersion != "" {
		return observedVersion
	}
	if manifestVersion, ok := exactConsumptionDependencyVersion(
		alert.Ecosystem,
		consumption.Lockfile,
		consumption.InstalledVersion,
		consumption.DependencyRange,
	); ok {
		return manifestVersion
	}
	return ""
}

func securityAlertObservedVersionMissingEvidence(
	consumption SecurityAlertConsumption,
	observedVersion string,
) []string {
	if securityAlertObservedVersionLooksMalformed(observedVersion) {
		return []string{securityAlertInstalledVersionMalformed}
	}
	if strings.TrimSpace(observedVersion) != "" {
		return nil
	}
	dependencyRange := strings.TrimSpace(consumption.DependencyRange)
	requestedRange := strings.TrimSpace(consumption.RequestedRange)
	if dependencyRange == "" && requestedRange == "" {
		return []string{securityAlertInstalledVersionMissing}
	}
	if securityAlertVersionTextLooksLikeRange(dependencyRange) ||
		securityAlertVersionTextLooksLikeRange(requestedRange) {
		return []string{securityAlertInstalledVersionMissing}
	}
	return []string{securityAlertInstalledVersionMalformed}
}

func securityAlertObservedVersionLooksMalformed(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" || securityAlertVersionTextLooksLikeRange(value) {
		return false
	}
	for _, char := range value {
		if char >= '0' && char <= '9' {
			return false
		}
	}
	return true
}

func securityAlertVersionTextLooksLikeRange(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	return strings.ContainsAny(value, "<>^~*=|, []") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") ||
		nonVersionDependencyPrefix(lower)
}

// securityAlertConsumptionEvidenceKind declares locally rather than importing
// the reducer root's securityAlertConsumptionEvidenceKind
// (supply_chain_impact_security_alert.go): a short fallback shared with
// supply_chain's own finding assembly, which has not moved out of root yet
// (issue #6061), for the same reason as the helpers in
// security_alert_reconciliation.go.
func securityAlertConsumptionEvidenceKind(consumption SecurityAlertConsumption) string {
	if strings.TrimSpace(consumption.EvidenceKind) != "" {
		return strings.TrimSpace(consumption.EvidenceKind)
	}
	return factschema.FactKindReducerPackageConsumptionCorrelation
}

// exactConsumptionDependencyVersion, exactManifestDependencyVersion, and
// nonVersionDependencyPrefix declare locally rather than importing the
// reducer root's versions (all three in supply_chain_impact_ranges.go): pure
// version-string classification
// with no reducer-root state, shared with supply_chain's own observed-version
// resolution, which has not moved out of root yet (issue #6061).
// exactManifestDependencyVersion and nonVersionDependencyPrefix are
// byte-identical to the root originals. exactConsumptionDependencyVersion is
// not: it takes only the three SecurityAlertConsumption fields the logic
// actually reads (lockfile, installedVersion, dependencyRange) instead of the
// root's full supplyChainPackageConsumption value type, and inlines the root's
// one-line normalizedSupplyChainVersionEcosystem. The behaviour is equivalent;
// the body is not the same text.
func exactConsumptionDependencyVersion(ecosystem string, lockfile bool, installedVersion, dependencyRange string) (string, bool) {
	switch string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem))) {
	case string(packageidentity.EcosystemCargo), string(packageidentity.EcosystemNuGet):
		if !lockfile {
			return "", false
		}
	}
	if version, ok := exactManifestDependencyVersion(installedVersion); ok {
		return version, true
	}
	if lockfile {
		version := strings.TrimSpace(dependencyRange)
		return version, version != ""
	}
	return exactManifestDependencyVersion(dependencyRange)
}

func exactManifestDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || nonVersionDependencyPrefix(lower) {
		return "", false
	}
	if strings.ContainsAny(version, "<>^~*=|, []") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(version, "$") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return "", false
	}
	return version, true
}

func nonVersionDependencyPrefix(lower string) bool {
	for _, prefix := range []string{
		"file:",
		"git+",
		"github:",
		"http:",
		"https:",
		"link:",
		"npm:",
		"portal:",
		"workspace:",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
