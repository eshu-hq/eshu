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
		consumption.DependencyResolutionState,
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
		nonVersionDependencyPrefix(lower) ||
		payloadcore.GradleVersionSelector(value)
}

// securityAlertConsumptionEvidenceKind declares locally rather than importing
// the reducer root's securityAlertConsumptionEvidenceKind
// (security_alert.go): a short fallback shared with
// supplychain/core's own finding assembly (issue #6061),
// for the same reason as the helpers in
// reconciliation.go.
func securityAlertConsumptionEvidenceKind(consumption SecurityAlertConsumption) string {
	if strings.TrimSpace(consumption.EvidenceKind) != "" {
		return strings.TrimSpace(consumption.EvidenceKind)
	}
	return factschema.FactKindReducerPackageConsumptionCorrelation
}

// exactConsumptionDependencyVersion and nonVersionDependencyPrefix declare
// locally rather than importing the reducer root's versions (both in
// ranges.go): pure version-string classification
// with no reducer-root state, shared with supply_chain's own observed-version
// resolution, which has not moved out of root yet (issue #6061).
// exactManifestDependencyVersion forwards to payloadcore, which owns the
// shared rule. nonVersionDependencyPrefix is
// byte-identical to the root original. exactConsumptionDependencyVersion is
// not: it takes only the four SecurityAlertConsumption fields the logic
// actually reads (dependencyResolutionState, lockfile, installedVersion,
// dependencyRange) instead of the
// full supplychainmodel.PackageConsumption value type, and inlines the root's
// one-line normalizedSupplyChainVersionEcosystem. The behaviour is equivalent;
// the body is not the same text.
func exactConsumptionDependencyVersion(ecosystem, resolutionState string, lockfile bool, installedVersion, dependencyRange string) (string, bool) {
	if strings.EqualFold(strings.TrimSpace(resolutionState), "unresolved") {
		// The parser left the version text as written; it is not a version
		// (issue #8988). The same rule gates supplychain/core.
		return "", false
	}
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

// exactManifestDependencyVersion forwards to
// [payloadcore.ExactManifestDependencyVersion] so the security-alert path and
// package correlation share one exact-version rule (issue #8988).
func exactManifestDependencyVersion(raw string) (string, bool) {
	return payloadcore.ExactManifestDependencyVersion(raw)
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
