// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// appendSecurityAlertImpactFindings seeds supply-chain-impact findings from
// provider security alerts, decoding the security_alert.repository_alert facts
// through the same typed contracts seam the reconciliation read surface uses.
//
// It returns the []quarantinedFact for any alert whose payload was missing its
// required repository_id identity anchor, and a fatal error for a non-decode
// failure, so SupplyChainImpactHandler.Handle applies the SAME per-fact
// isolation to a malformed security alert that it already applies to a
// malformed vulnerability fact: the poisoned alert is skipped and recorded as a
// visible input_invalid dead-letter while every valid sibling still seeds its
// finding and the whole supply_chain_impact generation still publishes. A
// malformed alert therefore can no longer produce an empty-identity impact
// finding (securityAlertCanSeedImpact already required a non-empty RepositoryID,
// so a repository_id-less alert never seeded a finding — but the pre-typing
// path dropped it silently; it now dead-letters visibly).
func appendSecurityAlertImpactFindings(
	findings []SupplyChainImpactFinding,
	envelopes []facts.Envelope,
	index supplyChainImpactIndex,
) ([]SupplyChainImpactFinding, []quarantinedFact, error) {
	alerts, quarantined, err := extractProviderSecurityAlertsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	if len(alerts) == 0 {
		return findings, quarantined, nil
	}
	consumptions := extractSecurityAlertConsumptions(envelopes)
	consumptions = append(consumptions, extractSecurityAlertManifestConsumptions(alerts, envelopes)...)
	for _, alert := range alerts {
		finding, ok := buildSecurityAlertImpactFinding(alert, consumptions, findings, index)
		if !ok {
			continue
		}
		findings = appendSupplyChainImpactFinding(findings, finding)
	}
	return findings, quarantined, nil
}

func buildSecurityAlertImpactFinding(
	alert providerSecurityAlert,
	consumptions []securityAlertConsumption,
	existing []SupplyChainImpactFinding,
	index supplyChainImpactIndex,
) (SupplyChainImpactFinding, bool) {
	if !securityAlertCanSeedImpact(alert) {
		return SupplyChainImpactFinding{}, false
	}
	consumption, _, ambiguousConsumption := matchSecurityAlertConsumption(alert, consumptions)
	if consumption.factID == "" || ambiguousConsumption {
		return SupplyChainImpactFinding{}, false
	}
	if securityAlertImpactAlreadyExists(alert, consumption.repositoryID, existing) {
		return SupplyChainImpactFinding{}, false
	}

	observedVersion := strings.TrimSpace(consumption.observedVersion)
	if observedVersion == "" {
		if manifestVersion, ok := exactConsumptionDependencyVersion(alert.Ecosystem, supplyChainPackageConsumption{
			dependencyRange: consumption.dependencyRange,
			lockfile:        consumption.lockfile,
		}); ok {
			observedVersion = manifestVersion
		}
	}
	requestedRange := firstNonBlank(
		strings.TrimSpace(consumption.requestedRange),
		strings.TrimSpace(consumption.dependencyRange),
	)
	pkg := supplyChainAffectedPackageFromSecurityAlert(alert)
	decision := evaluateSupplyChainVersionMatch(
		alert.Ecosystem,
		observedVersion,
		requestedRange,
		alert.PatchedVersion,
		[]supplyChainAffectedPackage{pkg},
	)
	finding := SupplyChainImpactFinding{
		CVEID:              securityAlertImpactCVEID(alert),
		AdvisoryID:         securityAlertImpactAdvisoryID(alert),
		PackageID:          alert.PackageID,
		Ecosystem:          strings.ToLower(strings.TrimSpace(alert.Ecosystem)),
		PackageName:        alert.PackageName,
		ObservedVersion:    observedVersion,
		RequestedRange:     requestedRange,
		FixedVersion:       strings.TrimSpace(alert.PatchedVersion),
		VulnerableRange:    strings.TrimSpace(alert.VulnerableRange),
		CVSSScore:          securityAlertCVSSScore(alert.CVSS),
		SeveritySource:     strings.TrimSpace(alert.Provider),
		SeverityVector:     securityAlertCVSSVector(alert.CVSS),
		SeverityLabel:      strings.ToLower(strings.TrimSpace(alert.Severity)),
		AdvisoryUpdatedAt:  strings.TrimSpace(alert.UpdatedAt),
		FixedVersionSource: strings.TrimSpace(alert.Provider),
		RangeSource:        strings.TrimSpace(alert.Provider),
		RepositoryID:       consumption.repositoryID,
		DependencyScope:    firstNonBlank(strings.TrimSpace(consumption.dependencyScope), strings.TrimSpace(alert.DependencyScope)),
		DependencyPath:     append([]string(nil), consumption.dependencyPath...),
		DependencyDepth:    consumption.dependencyDepth,
		DirectDependency:   cloneBoolPointer(consumption.directDependency),
		EvidencePath: []string{
			facts.SecurityAlertRepositoryAlertFactKind,
			securityAlertConsumptionEvidenceKind(consumption),
		},
		EvidenceFactIDs: []string{alert.ProviderAlertFactID, consumption.factID},
		CanonicalWrites: 1,
		AdvisorySources: []AdvisorySourceObservation{{
			Source:          strings.TrimSpace(alert.Provider),
			AdvisoryID:      securityAlertImpactAdvisoryID(alert),
			SourceUpdatedAt: strings.TrimSpace(alert.UpdatedAt),
		}},
	}
	applySupplyChainVersionDecision(&finding, decision)
	finalizeSupplyChainImpactFinding(&finding, index, decision.MissingEvidence)
	return finding, true
}

func securityAlertConsumptionEvidenceKind(consumption securityAlertConsumption) string {
	if strings.TrimSpace(consumption.evidenceKind) != "" {
		return strings.TrimSpace(consumption.evidenceKind)
	}
	return packageConsumptionCorrelationFactKind
}

func securityAlertCanSeedImpact(alert providerSecurityAlert) bool {
	switch strings.ToLower(strings.TrimSpace(alert.ProviderState)) {
	case "dismissed", "auto_dismissed", "fixed":
		return false
	}
	return strings.TrimSpace(alert.RepositoryID) != "" &&
		strings.TrimSpace(alert.PackageID) != "" &&
		strings.TrimSpace(securityAlertImpactAdvisoryID(alert)) != ""
}

func supplyChainAffectedPackageFromSecurityAlert(alert providerSecurityAlert) supplyChainAffectedPackage {
	return supplyChainAffectedPackage{
		factID:           alert.ProviderAlertFactID,
		cveID:            securityAlertImpactCVEID(alert),
		source:           strings.TrimSpace(alert.Provider),
		advisoryID:       securityAlertImpactAdvisoryID(alert),
		packageID:        alert.PackageID,
		ecosystem:        strings.ToLower(strings.TrimSpace(alert.Ecosystem)),
		name:             alert.PackageName,
		affectedRangeRaw: normalizeSecurityAlertComparatorRange(alert.VulnerableRange),
		fixedVersions:    compactStringSlice(alert.PatchedVersion),
	}
}

func securityAlertImpactAlreadyExists(
	alert providerSecurityAlert,
	repositoryID string,
	findings []SupplyChainImpactFinding,
) bool {
	for _, finding := range findings {
		if finding.RepositoryID != repositoryID || finding.PackageID != alert.PackageID {
			continue
		}
		if securityAlertIDMatches(alert.CVEIDs, finding.CVEID) ||
			securityAlertIDMatches(alert.GHSAIDs, finding.AdvisoryID) {
			return true
		}
	}
	return false
}

func securityAlertImpactCVEID(alert providerSecurityAlert) string {
	return firstNonBlank(firstSecurityAlertID(alert.CVEIDs), firstSecurityAlertID(alert.GHSAIDs))
}

func securityAlertImpactAdvisoryID(alert providerSecurityAlert) string {
	return firstNonBlank(firstSecurityAlertID(alert.GHSAIDs), firstSecurityAlertID(alert.CVEIDs), alert.ProviderAlertID)
}

func firstSecurityAlertID(values []string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func securityAlertCVSSScore(values map[string]any) float64 {
	if values == nil {
		return 0
	}
	return supplyChainFloat(values, "score")
}

func securityAlertCVSSVector(values map[string]any) string {
	if values == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(values["vector"]))
}

func normalizeSecurityAlertComparatorRange(raw string) string {
	fields := strings.Fields(strings.ReplaceAll(strings.TrimSpace(raw), ",", " "))
	if len(fields) == 0 {
		return ""
	}
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := strings.TrimSpace(fields[i])
		if securityAlertComparatorOperator(field) && i+1 < len(fields) {
			out = append(out, field+strings.TrimSpace(fields[i+1]))
			i++
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func securityAlertComparatorOperator(value string) bool {
	switch strings.TrimSpace(value) {
	case ">", ">=", "<", "<=", "=", "==", "!=":
		return true
	default:
		return false
	}
}
