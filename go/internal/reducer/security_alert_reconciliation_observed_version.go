// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

const (
	securityAlertInstalledVersionMissing   = "installed package version missing"
	securityAlertInstalledVersionMalformed = "installed package version malformed"
)

func applySecurityAlertDependencyEvidence(
	decision *SecurityAlertReconciliationDecision,
	alert providerSecurityAlert,
	consumption securityAlertConsumption,
) {
	decision.DependencyEvidenceID = consumption.factID
	decision.DependencyEvidenceKind = securityAlertConsumptionEvidenceKind(consumption)
	decision.DependencyRange = strings.TrimSpace(consumption.dependencyRange)
	decision.RequestedRange = firstNonBlank(
		strings.TrimSpace(consumption.requestedRange),
		decision.DependencyRange,
	)
	decision.ObservedVersion = securityAlertObservedVersion(alert, consumption)
	decision.PackageMissingEvidence = uniqueSortedStrings(append(
		decision.PackageMissingEvidence,
		securityAlertObservedVersionMissingEvidence(consumption, decision.ObservedVersion)...,
	))
}

func securityAlertObservedVersion(
	alert providerSecurityAlert,
	consumption securityAlertConsumption,
) string {
	if observedVersion := strings.TrimSpace(consumption.observedVersion); observedVersion != "" {
		return observedVersion
	}
	if manifestVersion, ok := exactConsumptionDependencyVersion(alert.Ecosystem, supplyChainPackageConsumption{
		dependencyRange:  consumption.dependencyRange,
		requestedRange:   consumption.requestedRange,
		observedVersion:  consumption.observedVersion,
		installedVersion: consumption.installedVersion,
		dependencyScope:  consumption.dependencyScope,
		lockfile:         consumption.lockfile,
		dependencyDepth:  consumption.dependencyDepth,
		directDependency: consumption.directDependency,
	}); ok {
		return manifestVersion
	}
	return ""
}

func securityAlertObservedVersionMissingEvidence(
	consumption securityAlertConsumption,
	observedVersion string,
) []string {
	if securityAlertObservedVersionLooksMalformed(observedVersion) {
		return []string{securityAlertInstalledVersionMalformed}
	}
	if strings.TrimSpace(observedVersion) != "" {
		return nil
	}
	dependencyRange := strings.TrimSpace(consumption.dependencyRange)
	requestedRange := strings.TrimSpace(consumption.requestedRange)
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
