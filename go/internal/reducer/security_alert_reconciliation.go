// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SecurityAlertReconciliationDecision is one reducer-owned comparison between
// a provider-reported repository alert and Eshu-owned evidence.
type SecurityAlertReconciliationDecision struct {
	ProviderAlertFactID         string
	ProviderAlertScopeID        string
	ProviderAlertGenerationID   string
	Provider                    string
	ProviderAlertID             string
	ProviderAlertNumber         int64
	ProviderState               string
	RepositoryID                string
	ProviderRepositoryID        string
	RepositoryName              string
	PackageID                   string
	Ecosystem                   string
	PackageName                 string
	ManifestPath                string
	DependencyScope             string
	Relationship                string
	GHSAIDs                     []string
	CVEIDs                      []string
	VulnerableRange             string
	PatchedVersion              string
	Severity                    string
	CVSS                        map[string]any
	EPSS                        map[string]string
	CWEs                        []map[string]string
	Summary                     string
	SourceURL                   string
	CreatedAt                   string
	UpdatedAt                   string
	FixedAt                     string
	DismissedAt                 string
	SourceFreshness             string
	CollectionCoverageState     string
	CollectionTruncated         bool
	CollectionPagesFetched      int64
	CollectionStateFilter       string
	CollectionIncompleteReasons []string
	Status                      SecurityAlertReconciliationStatus
	EshuImpactStatus            string
	EshuImpactFindingID         string
	ObservedVersion             string
	RequestedRange              string
	DependencyRange             string
	Reason                      string
	ReasonCode                  string
	MissingEvidence             []SecurityAlertReconciliationMissingEvidence
	PackageMissingEvidence      []string
	CanonicalWrites             int
	EvidenceFactIDs             []string
	DependencyEvidenceID        string
	DependencyEvidenceKind      string
	ImpactEvidenceID            string
}

// BuildSecurityAlertReconciliations compares provider-reported repository
// alerts to active Eshu dependency and impact facts without changing
// supply-chain impact admission.
//
// It keeps its pre-typing signature (no error, no quarantine count) because it
// is the entry point the security_alert_reconciliation table tests exercise
// directly. A security_alert.repository_alert fact missing its required
// repository_id is excluded from the alert set (matching the pre-typing
// behavior of dropping a fact with a blank required string), while the reducer
// intent path calls the quarantine-aware
// BuildSecurityAlertReconciliationsWithQuarantine so a malformed fact still
// surfaces as a visible input_invalid dead-letter. A non-input_invalid decode
// error (unsupported schema major, undecodable shape) is dropped here too and
// re-surfaced fatally by the WithQuarantine variant.
func BuildSecurityAlertReconciliations(envelopes []facts.Envelope) []SecurityAlertReconciliationDecision {
	decisions, _, _ := BuildSecurityAlertReconciliationsWithQuarantine(envelopes)
	return decisions
}

// BuildSecurityAlertReconciliationsWithQuarantine is the quarantine-aware
// counterpart BuildSecurityAlertReconciliations delegates to and
// SecurityAlertReconciliationHandler.Handle calls directly, so the reducer
// intent path can report each malformed security_alert.repository_alert fact as
// a visible input_invalid dead-letter via recordQuarantinedFacts. A non-decode
// error (a fatal condition partitionDecodeFailures did not quarantine) is
// returned so the caller fails the whole intent for durable triage. The
// classification logic itself is unchanged from the pre-typing build.
func BuildSecurityAlertReconciliationsWithQuarantine(
	envelopes []facts.Envelope,
) ([]SecurityAlertReconciliationDecision, []quarantinedFact, error) {
	alerts, quarantined, err := extractProviderSecurityAlertsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	consumptions := extractSecurityAlertConsumptions(envelopes)
	consumptions = append(consumptions, extractSecurityAlertManifestConsumptions(alerts, envelopes)...)
	impacts := extractSecurityAlertImpacts(envelopes)

	decisions := make([]SecurityAlertReconciliationDecision, 0, len(alerts))
	for _, alert := range alerts {
		decisions = append(decisions, classifyProviderSecurityAlert(alert, consumptions, impacts))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].RepositoryID != decisions[j].RepositoryID {
			return decisions[i].RepositoryID < decisions[j].RepositoryID
		}
		if decisions[i].Provider != decisions[j].Provider {
			return decisions[i].Provider < decisions[j].Provider
		}
		return decisions[i].ProviderAlertNumber < decisions[j].ProviderAlertNumber
	})
	return decisions, quarantined, nil
}

func extractSecurityAlertConsumptions(envelopes []facts.Envelope) []securityAlertConsumption {
	consumptions := make([]securityAlertConsumption, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != packageConsumptionCorrelationFactKind {
			continue
		}
		consumptions = append(consumptions, securityAlertConsumption{
			factID:           envelope.FactID,
			evidenceKind:     packageConsumptionCorrelationFactKind,
			repositoryID:     payloadStr(envelope.Payload, "repository_id"),
			repositoryName:   payloadStr(envelope.Payload, "repository_name"),
			packageID:        payloadStr(envelope.Payload, "package_id"),
			relativePath:     payloadStr(envelope.Payload, "relative_path"),
			observedAt:       envelope.ObservedAt,
			dependencyRange:  payloadStr(envelope.Payload, "dependency_range"),
			observedVersion:  payloadStr(envelope.Payload, "observed_version"),
			installedVersion: payloadStr(envelope.Payload, "installed_version"),
			requestedRange:   payloadStr(envelope.Payload, "requested_range"),
			dependencyPath:   payloadOrderedStrings(envelope.Payload, "dependency_path"),
			dependencyDepth:  supplyChainInt(envelope.Payload, "dependency_depth"),
			directDependency: payloadBoolPointer(envelope.Payload, "direct_dependency"),
			dependencyScope:  supplyChainDependencyScope(envelope.Payload),
			lockfile:         payloadBool(envelope.Payload, "lockfile"),
		})
	}
	return consumptions
}

func extractSecurityAlertManifestConsumptions(
	alerts []providerSecurityAlert,
	envelopes []facts.Envelope,
) []securityAlertConsumption {
	if len(alerts) == 0 {
		return nil
	}
	dependencies := extractPackageManifestDependencies(envelopes)
	if len(dependencies) == 0 {
		return nil
	}
	consumptions := make([]securityAlertConsumption, 0)
	for _, dependency := range dependencies {
		for _, alert := range alerts {
			if !securityAlertManifestConsumptionMatches(alert, dependency) {
				continue
			}
			consumptions = append(consumptions, securityAlertConsumption{
				factID:           dependency.FactID,
				evidenceKind:     factKindContentEntity,
				repositoryID:     dependency.RepositoryID,
				repositoryName:   dependency.RepositoryName,
				packageID:        alert.PackageID,
				relativePath:     dependency.RelativePath,
				observedAt:       dependency.ObservedAt,
				dependencyRange:  dependency.DependencyRange,
				observedVersion:  dependency.ObservedVersion,
				installedVersion: dependency.InstalledVersion,
				requestedRange:   dependency.RequestedRange,
				dependencyPath:   append([]string(nil), dependency.DependencyPath...),
				dependencyDepth:  dependency.DependencyDepth,
				directDependency: cloneBoolPointer(dependency.DirectDependency),
				dependencyScope:  dependency.DependencyScope,
				lockfile:         dependency.Lockfile,
			})
		}
	}
	return consumptions
}

func securityAlertManifestConsumptionMatches(
	alert providerSecurityAlert,
	dependency packageManifestDependency,
) bool {
	if strings.TrimSpace(alert.PackageID) == "" ||
		!securityAlertRepositoryScopeMatches(alert, securityAlertConsumption{
			repositoryID:   dependency.RepositoryID,
			repositoryName: dependency.RepositoryName,
		}) {
		return false
	}
	alertKeys := stringSet(packageConsumptionKeys(
		alert.Ecosystem,
		securityAlertPackageNameCandidates(alert)...,
	))
	if len(alertKeys) == 0 {
		return false
	}
	for _, key := range packageConsumptionKeys(dependency.PackageManager, packageManifestDependencyNameCandidates(dependency)...) {
		if _, ok := alertKeys[key]; ok {
			return true
		}
	}
	return false
}

func securityAlertPackageNameCandidates(alert providerSecurityAlert) []string {
	candidates := []string{alert.PackageName}
	packageID := strings.TrimSpace(alert.PackageID)
	if strings.HasPrefix(packageID, "pkg:") {
		candidates = append(candidates, packageNameFromPURL(packageID))
	} else {
		candidates = append(candidates, packageNameFromPackageID(packageID))
	}
	return uniqueSortedStrings(candidates)
}

func extractSecurityAlertImpacts(envelopes []facts.Envelope) []securityAlertImpact {
	impacts := make([]securityAlertImpact, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != supplyChainImpactFactKind {
			continue
		}
		impacts = append(impacts, securityAlertImpact{
			factID:       envelope.FactID,
			repositoryID: payloadStr(envelope.Payload, "repository_id"),
			packageID:    payloadStr(envelope.Payload, "package_id"),
			cveID:        payloadStr(envelope.Payload, "cve_id"),
			advisoryID:   payloadStr(envelope.Payload, "advisory_id"),
			status:       payloadStr(envelope.Payload, "impact_status"),
		})
	}
	return impacts
}

func classifyProviderSecurityAlert(
	alert providerSecurityAlert,
	consumptions []securityAlertConsumption,
	impacts []securityAlertImpact,
) SecurityAlertReconciliationDecision {
	decision := alert.SecurityAlertReconciliationDecision
	switch decision.ProviderState {
	case "dismissed", "auto_dismissed":
		decision.Status = SecurityAlertReconciliationDismissed
		decision.Reason = "provider alert is dismissed at the source"
		decision.ReasonCode = securityAlertReasonProviderDismissed
		return decision
	case "fixed":
		decision.Status = SecurityAlertReconciliationFixed
		decision.Reason = "provider alert is fixed at the source"
		decision.ReasonCode = securityAlertReasonProviderFixed
		return decision
	}

	exactConsumption, staleConsumption, ambiguousConsumption := matchSecurityAlertConsumption(alert, consumptions)
	if exactConsumption.factID == "" {
		if ambiguousConsumption {
			decision.Status = SecurityAlertReconciliationAmbiguous
			decision.Reason = "provider alert repository scope is ambiguous across owned dependency evidence"
			decision.ReasonCode = securityAlertReasonOwnedDependencyAmbig
			decision.MissingEvidence = securityAlertMissingEvidence(
				"owned_dependency",
				"multiple_repository_candidates",
				"",
			)
			return decision
		}
		if staleConsumption.factID != "" {
			decision.RepositoryID = staleConsumption.repositoryID
			decision.RepositoryName = firstNonBlank(staleConsumption.repositoryName, decision.RepositoryName)
			decision.Status = SecurityAlertReconciliationStale
			applySecurityAlertDependencyEvidence(&decision, alert, staleConsumption)
			decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, staleConsumption.factID))
			decision.Reason = "newer owned dependency evidence no longer matches the provider alert manifest path"
			decision.ReasonCode = securityAlertReasonProviderAlertStale
			decision.MissingEvidence = securityAlertMissingEvidence(
				"current_manifest",
				"provider_manifest_no_longer_observed",
				staleConsumption.factID,
			)
			return decision
		}
		if status, reasonCode, missing, ok := securityAlertUnsupportedTriage(alert); ok {
			decision.Status = status
			decision.Reason = "provider alert ecosystem is unsupported by the current Eshu impact matcher"
			decision.ReasonCode = reasonCode
			decision.MissingEvidence = missing
			return decision
		}
		decision.Status = SecurityAlertReconciliationProviderOnly
		decision.Reason = "provider alert has no matching owned dependency evidence"
		decision.ReasonCode = securityAlertReasonOwnedDependencyMissed
		decision.MissingEvidence = securityAlertMissingEvidence(
			"owned_dependency",
			"no_owned_dependency_evidence",
			"",
		)
		return decision
	}
	decision.RepositoryID = exactConsumption.repositoryID
	decision.RepositoryName = firstNonBlank(exactConsumption.repositoryName, decision.RepositoryName)
	applySecurityAlertDependencyEvidence(&decision, alert, exactConsumption)
	decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, exactConsumption.factID))
	if status, reasonCode, missing, ok := securityAlertUnsupportedTriage(alert); ok {
		decision.Status = status
		decision.Reason = "provider alert ecosystem is unsupported by the current Eshu impact matcher"
		decision.ReasonCode = reasonCode
		decision.MissingEvidence = missing
		return decision
	}

	alert.RepositoryID = exactConsumption.repositoryID
	impact := matchSecurityAlertImpact(alert, impacts)
	if impact.factID == "" {
		decision.Status = SecurityAlertReconciliationUnmatched
		decision.Reason = "owned dependency exists but no reducer impact finding matches the provider advisory identifiers"
		decision.ReasonCode = securityAlertReasonImpactFindingMissing
		decision.MissingEvidence = securityAlertMissingEvidence(
			"impact_finding",
			"no_matching_impact_finding",
			exactConsumption.factID,
		)
		return decision
	}
	decision.Status = SecurityAlertReconciliationMatched
	decision.EshuImpactStatus = impact.status
	decision.EshuImpactFindingID = impact.factID
	decision.ImpactEvidenceID = impact.factID
	decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, impact.factID))
	decision.Reason = "provider alert matches owned dependency and reducer impact evidence"
	decision.ReasonCode = securityAlertReasonMatchedExactImpact
	return decision
}

func matchSecurityAlertConsumption(
	alert providerSecurityAlert,
	consumptions []securityAlertConsumption,
) (securityAlertConsumption, securityAlertConsumption, bool) {
	var exactCandidates []securityAlertConsumption
	var staleCandidates []securityAlertConsumption
	for _, consumption := range consumptions {
		if !securityAlertRepositoryScopeMatches(alert, consumption) || consumption.packageID != alert.PackageID {
			continue
		}
		if alert.ManifestPath == "" || consumption.relativePath == alert.ManifestPath {
			exactCandidates = append(exactCandidates, consumption)
			continue
		}
		if !alert.updatedAtTime.IsZero() && consumption.observedAt.After(alert.updatedAtTime) {
			staleCandidates = append(staleCandidates, consumption)
		}
	}
	exact, exactAmbiguous := selectSecurityAlertConsumption(exactCandidates)
	stale, staleAmbiguous := selectSecurityAlertConsumption(staleCandidates)
	return exact, stale, exactAmbiguous || (exact.factID == "" && staleAmbiguous)
}

func securityAlertRepositoryScopeMatches(
	alert providerSecurityAlert,
	consumption securityAlertConsumption,
) bool {
	alertRepositoryID := strings.TrimSpace(alert.RepositoryID)
	consumptionRepositoryID := strings.TrimSpace(consumption.repositoryID)
	if alertRepositoryID != "" && consumptionRepositoryID != "" && alertRepositoryID == consumptionRepositoryID {
		return true
	}
	alertRepositoryName := normalizeSecurityAlertRepositoryName(alert.RepositoryName)
	consumptionRepositoryName := normalizeSecurityAlertRepositoryName(consumption.repositoryName)
	return alertRepositoryName != "" && alertRepositoryName == consumptionRepositoryName
}

func selectSecurityAlertConsumption(candidates []securityAlertConsumption) (securityAlertConsumption, bool) {
	if len(candidates) == 0 {
		return securityAlertConsumption{}, false
	}
	selected := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.repositoryID != selected.repositoryID {
			return securityAlertConsumption{}, true
		}
		if securityAlertConsumptionIsNewerStaleCandidate(candidate, selected) {
			selected = candidate
		}
	}
	return selected, false
}

func securityAlertConsumptionIsNewerStaleCandidate(
	candidate securityAlertConsumption,
	current securityAlertConsumption,
) bool {
	if current.factID == "" {
		return true
	}
	if candidate.observedAt.After(current.observedAt) {
		return true
	}
	if candidate.observedAt.Equal(current.observedAt) {
		return candidate.factID < current.factID
	}
	return false
}

func normalizeSecurityAlertRepositoryName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func securityAlertRepositoryNameFromID(repositoryID string) string {
	trimmed := strings.TrimSpace(repositoryID)
	if trimmed == "" {
		return ""
	}
	if slash := strings.LastIndex(trimmed, "/"); slash >= 0 && slash+1 < len(trimmed) {
		return strings.TrimSpace(trimmed[slash+1:])
	}
	return ""
}

func matchSecurityAlertImpact(alert providerSecurityAlert, impacts []securityAlertImpact) securityAlertImpact {
	for _, impact := range impacts {
		if impact.repositoryID != alert.RepositoryID || impact.packageID != alert.PackageID {
			continue
		}
		if securityAlertIDMatches(alert.CVEIDs, impact.cveID) ||
			securityAlertIDMatches(alert.GHSAIDs, impact.advisoryID) {
			return impact
		}
	}
	return securityAlertImpact{}
}

func securityAlertIDMatches(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
