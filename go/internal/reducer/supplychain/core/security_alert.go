// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core/cvss"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// appendSecurityAlertImpactFindings seeds supply-chain-impact findings from
// provider security alerts, decoding the security_alert.repository_alert facts
// through the same typed contracts seam the reconciliation read surface uses.
//
// It returns the []factdecode.QuarantinedFact for any alert whose payload was missing its
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
) ([]SupplyChainImpactFinding, []factdecode.QuarantinedFact, error) {
	alerts, quarantined, err := securityalert.ExtractProviderSecurityAlertsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	if len(alerts) == 0 {
		return findings, quarantined, nil
	}
	consumptions := securityalert.ExtractSecurityAlertConsumptions(envelopes)
	consumptions = append(consumptions, correlation.ExtractSecurityAlertManifestConsumptions(alerts, envelopes)...)
	for _, alert := range alerts {
		findings = appendSecurityAlertVersionFindings(findings, alert, consumptions, index)
	}
	return findings, quarantined, nil
}

// appendSecurityAlertVersionFindings classifies one finding per installed
// version of the alerted package in the alert's manifest (#11121). One pick
// per alert judged every version on the row with the lower fact id, so a
// fixed row could hide an affected one. The exact candidates split by
// observed version (splitSecurityAlertConsumptionsByVersion), and
// SelectSecurityAlertConsumption picks the row within each version by the
// rule MatchSecurityAlertConsumption uses. An ambiguous match yields nothing,
// as before. Findings already in findings (the advisory path's, and earlier
// alerts') are the ones securityAlertImpactAlreadyExists checks against.
func appendSecurityAlertVersionFindings(
	findings []SupplyChainImpactFinding,
	alert securityalert.ProviderSecurityAlert,
	consumptions []securityalert.SecurityAlertConsumption,
	index supplyChainImpactIndex,
) []SupplyChainImpactFinding {
	if !securityAlertCanSeedImpact(alert) {
		return findings
	}
	candidates, ambiguous := securityalert.ExactSecurityAlertConsumptions(alert, consumptions)
	if ambiguous || len(candidates) == 0 {
		return findings
	}
	for _, group := range splitSecurityAlertConsumptionsByVersion(alert.Ecosystem, candidates) {
		consumption, groupAmbiguous := securityalert.SelectSecurityAlertConsumption(group)
		if groupAmbiguous {
			continue
		}
		if finding, ok := buildSecurityAlertImpactFinding(alert, consumption, findings, index); ok {
			findings = appendSupplyChainImpactFinding(findings, finding)
		}
	}
	return findings
}

// securityAlertObservedVersion is the observed version an alert consumption
// row gives its finding: the row's own observed version, else the exact
// version its installed version, lockfile, or manifest text proves. It reads
// InstalledVersion as exactConsumptionDependencyVersion does for the manifest
// path and as reconciliation does (#11121 review F4).
func securityAlertObservedVersion(ecosystem string, consumption securityalert.SecurityAlertConsumption) string {
	if observed := strings.TrimSpace(consumption.ObservedVersion); observed != "" {
		return observed
	}
	if manifestVersion, ok := exactConsumptionDependencyVersion(ecosystem, supplychainmodel.PackageConsumption{
		InstalledVersion:          consumption.InstalledVersion,
		DependencyRange:           consumption.DependencyRange,
		DependencyResolutionState: consumption.DependencyResolutionState,
		Lockfile:                  consumption.Lockfile,
	}); ok {
		return manifestVersion
	}
	return ""
}

// splitSecurityAlertConsumptionsByVersion groups an alert's exact candidates
// by observed version, ordered by version, so each installed version
// classifies once (#11121), the rule #11113 applies to the manifest path.
// When no candidate carries a version, all of them come back as one group:
// the pre-#11121 single pick. A candidate with no version beside versioned
// ones forms no group. The input is left as it was.
func splitSecurityAlertConsumptionsByVersion(
	ecosystem string,
	candidates []securityalert.SecurityAlertConsumption,
) [][]securityalert.SecurityAlertConsumption {
	if len(candidates) < 2 {
		return [][]securityalert.SecurityAlertConsumption{candidates}
	}
	byVersion := make(map[string][]securityalert.SecurityAlertConsumption)
	for _, candidate := range candidates {
		if version := securityAlertObservedVersion(ecosystem, candidate); version != "" {
			byVersion[version] = append(byVersion[version], candidate)
		}
	}
	if len(byVersion) == 0 {
		return [][]securityalert.SecurityAlertConsumption{candidates}
	}
	versions := slices.Sorted(maps.Keys(byVersion))
	out := make([][]securityalert.SecurityAlertConsumption, 0, len(versions))
	for _, version := range versions {
		out = append(out, byVersion[version])
	}
	return out
}

// buildSecurityAlertImpactFinding classifies alert against the one
// consumption row chosen for an installed version. It yields nothing when the
// row is empty or a finding for the same repository, package, advisory and
// observed version already exists.
func buildSecurityAlertImpactFinding(
	alert securityalert.ProviderSecurityAlert,
	consumption securityalert.SecurityAlertConsumption,
	existing []SupplyChainImpactFinding,
	index supplyChainImpactIndex,
) (SupplyChainImpactFinding, bool) {
	if consumption.FactID == "" {
		return SupplyChainImpactFinding{}, false
	}
	observedVersion := securityAlertObservedVersion(alert.Ecosystem, consumption)
	if securityAlertImpactAlreadyExists(alert, consumption.RepositoryID, observedVersion, existing) {
		return SupplyChainImpactFinding{}, false
	}
	requestedRange := payloadcore.FirstNonBlank(
		strings.TrimSpace(consumption.RequestedRange),
		strings.TrimSpace(consumption.DependencyRange),
	)
	pkg := supplyChainAffectedPackageFromSecurityAlert(alert)
	decision := evaluateSupplyChainVersionMatch(
		alert.Ecosystem,
		observedVersion,
		requestedRange,
		alert.PatchedVersion,
		[]supplychainmodel.AffectedPackage{pkg},
	)
	alertVector := securityAlertCVSSVector(alert.CVSS)
	alertLabel := strings.ToLower(strings.TrimSpace(alert.Severity))
	severity := cvss.DeriveSeverity(securityAlertCVSSScore(alert.CVSS), alertVector, alertLabel)
	finding := SupplyChainImpactFinding{
		CVEID:               securityAlertImpactCVEID(alert),
		AdvisoryID:          securityAlertImpactAdvisoryID(alert),
		PackageID:           alert.PackageID,
		Ecosystem:           packageidentity.LowerASCII(strings.TrimSpace(alert.Ecosystem)),
		PackageName:         alert.PackageName,
		ObservedVersion:     observedVersion,
		RequestedRange:      requestedRange,
		FixedVersion:        strings.TrimSpace(alert.PatchedVersion),
		VulnerableRange:     strings.TrimSpace(alert.VulnerableRange),
		CVSSScore:           severity.Score,
		SeverityScoreSource: derivedScoreSource(severity),
		SeveritySource:      strings.TrimSpace(alert.Provider),
		SeverityVector:      alertVector,
		SeverityLabel:       alertLabel,
		EPSSProbability:     securityAlertEPSSValue(alert.EPSS["percentage"]),
		EPSSPercentile:      securityAlertEPSSValue(alert.EPSS["percentile"]),
		AdvisoryUpdatedAt:   strings.TrimSpace(alert.UpdatedAt),
		FixedVersionSource:  strings.TrimSpace(alert.Provider),
		RangeSource:         strings.TrimSpace(alert.Provider),
		RepositoryID:        consumption.RepositoryID,
		DependencyScope:     payloadcore.FirstNonBlank(strings.TrimSpace(consumption.DependencyScope), strings.TrimSpace(alert.DependencyScope)),
		DependencyPath:      append([]string(nil), consumption.DependencyPath...),
		DependencyDepth:     consumption.DependencyDepth,
		DirectDependency:    cloneBoolPointer(consumption.DirectDependency),
		EvidencePath: []string{
			facts.SecurityAlertRepositoryAlertFactKind,
			securityAlertConsumptionEvidenceKind(consumption),
		},
		EvidenceFactIDs: []string{alert.ProviderAlertFactID, consumption.FactID},
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

// securityAlertEPSSValue keeps GitHub's decimal 0-1 EPSS values in source
// form and drops malformed or out-of-range values before they affect priority.
func securityAlertEPSSValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, "xXpP_") {
		return ""
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(parsed) || parsed < 0 || parsed > 1 {
		return ""
	}
	return value
}

func securityAlertConsumptionEvidenceKind(consumption securityalert.SecurityAlertConsumption) string {
	if strings.TrimSpace(consumption.EvidenceKind) != "" {
		return strings.TrimSpace(consumption.EvidenceKind)
	}
	return correlation.PackageConsumptionFactKind
}

func securityAlertCanSeedImpact(alert securityalert.ProviderSecurityAlert) bool {
	switch strings.ToLower(strings.TrimSpace(alert.ProviderState)) {
	case "dismissed", "auto_dismissed", "fixed":
		return false
	}
	return strings.TrimSpace(alert.RepositoryID) != "" &&
		strings.TrimSpace(alert.PackageID) != "" &&
		strings.TrimSpace(securityAlertImpactAdvisoryID(alert)) != ""
}

func supplyChainAffectedPackageFromSecurityAlert(alert securityalert.ProviderSecurityAlert) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		FactID:           alert.ProviderAlertFactID,
		CVEID:            securityAlertImpactCVEID(alert),
		Source:           strings.TrimSpace(alert.Provider),
		AdvisoryID:       securityAlertImpactAdvisoryID(alert),
		PackageID:        alert.PackageID,
		Ecosystem:        packageidentity.LowerASCII(strings.TrimSpace(alert.Ecosystem)),
		Name:             alert.PackageName,
		AffectedRangeRaw: normalizeSecurityAlertComparatorRange(alert.VulnerableRange),
		FixedVersions:    payloadcore.CompactStringSlice(alert.PatchedVersion),
	}
}

// securityAlertImpactAlreadyExists reports whether findings already hold this
// alert's advisory for the repository, package and observed version. It
// compares the version (#11121), so a finding for one installed version does
// not suppress the alert's finding for another. A row with no version keeps
// the pre-#11121 rule: any finding for the advisory suppresses it. The rule is
// not symmetric: an existing finding with no version no longer suppresses a
// versioned alert row, so a range-only advisory finding and the alert's
// version-proven finding both stand.
func securityAlertImpactAlreadyExists(
	alert securityalert.ProviderSecurityAlert,
	repositoryID string,
	observedVersion string,
	findings []SupplyChainImpactFinding,
) bool {
	for _, finding := range findings {
		if finding.RepositoryID != repositoryID || finding.PackageID != alert.PackageID {
			continue
		}
		if observedVersion != "" && strings.TrimSpace(finding.ObservedVersion) != observedVersion {
			continue
		}
		if securityalert.SecurityAlertIDMatches(alert.CVEIDs, finding.CVEID) ||
			securityalert.SecurityAlertIDMatches(alert.GHSAIDs, finding.AdvisoryID) {
			return true
		}
	}
	return false
}

func securityAlertImpactCVEID(alert securityalert.ProviderSecurityAlert) string {
	return payloadcore.FirstNonBlank(firstSecurityAlertID(alert.CVEIDs), firstSecurityAlertID(alert.GHSAIDs))
}

func securityAlertImpactAdvisoryID(alert securityalert.ProviderSecurityAlert) string {
	return payloadcore.FirstNonBlank(firstSecurityAlertID(alert.GHSAIDs), firstSecurityAlertID(alert.CVEIDs), alert.ProviderAlertID)
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
