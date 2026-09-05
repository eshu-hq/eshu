// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"net/url"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
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
func BuildSecurityAlertReconciliations(
	envelopes []facts.Envelope,
	extractManifestConsumptions ManifestConsumptionExtractor,
) []SecurityAlertReconciliationDecision {
	decisions, _, _ := BuildSecurityAlertReconciliationsWithQuarantine(envelopes, extractManifestConsumptions)
	return decisions
}

// ManifestConsumptionExtractor builds security-alert consumption evidence
// from repository manifest/lockfile dependency facts, matching each decoded
// provider alert's package identity against Eshu-observed manifest
// dependencies. It is injected rather than called directly because the
// manifest-dependency decode and package-identity normalization logic
// (extractPackageManifestDependencies, packageConsumptionKeys) is owned by
// the reducer root's package-consumption family, which has not moved out of
// root yet (issue #6061) -- a family subpackage may never import the reducer
// root, so root wires the concrete implementation into
// [SecurityAlertReconciliationHandler] and passes it to
// [BuildSecurityAlertReconciliationsWithQuarantine] at each call site
// instead.
type ManifestConsumptionExtractor func(alerts []ProviderSecurityAlert, envelopes []facts.Envelope) []SecurityAlertConsumption

// BuildSecurityAlertReconciliationsWithQuarantine is the quarantine-aware
// counterpart BuildSecurityAlertReconciliations delegates to and
// SecurityAlertReconciliationHandler.Handle calls directly, so the reducer
// intent path can report each malformed security_alert.repository_alert fact as
// a visible input_invalid dead-letter via recordQuarantinedFacts. A non-decode
// error (a fatal condition partitionDecodeFailures did not quarantine) is
// returned so the caller fails the whole intent for durable triage. The
// classification logic itself is unchanged from the pre-typing build.
// extractManifestConsumptions supplies the manifest/lockfile half of
// consumption evidence; see [ManifestConsumptionExtractor].
func BuildSecurityAlertReconciliationsWithQuarantine(
	envelopes []facts.Envelope,
	extractManifestConsumptions ManifestConsumptionExtractor,
) ([]SecurityAlertReconciliationDecision, []factdecode.QuarantinedFact, error) {
	alerts, quarantined, err := ExtractProviderSecurityAlertsWithQuarantine(envelopes)
	if err != nil {
		return nil, nil, err
	}
	consumptions := ExtractSecurityAlertConsumptions(envelopes)
	if extractManifestConsumptions != nil {
		consumptions = append(consumptions, extractManifestConsumptions(alerts, envelopes)...)
	}
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

// ExtractSecurityAlertConsumptions decodes every
// reducer_package_consumption_correlation fact in envelopes into a
// SecurityAlertConsumption. It is the non-manifest half of consumption
// evidence; BuildSecurityAlertReconciliationsWithQuarantine appends the
// manifest/lockfile half supplied by the caller's ManifestConsumptionExtractor
// to this function's result before matching. Envelopes of any other fact kind
// are skipped, so callers may pass an unfiltered envelope batch.
func ExtractSecurityAlertConsumptions(envelopes []facts.Envelope) []SecurityAlertConsumption {
	consumptions := make([]SecurityAlertConsumption, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != factschema.FactKindReducerPackageConsumptionCorrelation {
			continue
		}
		consumptions = append(consumptions, SecurityAlertConsumption{
			FactID:           envelope.FactID,
			EvidenceKind:     factschema.FactKindReducerPackageConsumptionCorrelation,
			RepositoryID:     payloadcore.PayloadStr(envelope.Payload, "repository_id"),
			RepositoryName:   payloadcore.PayloadStr(envelope.Payload, "repository_name"),
			PackageID:        payloadcore.PayloadStr(envelope.Payload, "package_id"),
			RelativePath:     payloadcore.PayloadStr(envelope.Payload, "relative_path"),
			ObservedAt:       envelope.ObservedAt,
			DependencyRange:  payloadcore.PayloadStr(envelope.Payload, "dependency_range"),
			ObservedVersion:  payloadcore.PayloadStr(envelope.Payload, "observed_version"),
			InstalledVersion: payloadcore.PayloadStr(envelope.Payload, "installed_version"),
			RequestedRange:   payloadcore.PayloadStr(envelope.Payload, "requested_range"),
			DependencyPath:   payloadcore.PayloadOrderedStrings(envelope.Payload, "dependency_path"),
			DependencyDepth:  payloadcore.PayloadInt(envelope.Payload, "dependency_depth"),
			DirectDependency: securityAlertPayloadBoolPointer(envelope.Payload, "direct_dependency"),
			DependencyScope:  securityAlertDependencyScope(envelope.Payload),
			Lockfile:         payloadcore.PayloadBool(envelope.Payload, "lockfile"),
		})
	}
	return consumptions
}

// SecurityAlertPackageNameCandidates returns the normalizable package-name
// forms for alert's identity (its raw PackageName plus a name parsed from
// PackageID, whether a purl or an Eshu package-registry URI). Exported for
// the reducer root's manifest-consumption bridge (issue #6061): the bridge
// still owns the package-consumption-keyed matching against manifest
// dependency evidence (see [ManifestConsumptionExtractor]) and needs the same
// alert-side candidates this package uses for non-manifest consumption
// matching.
func SecurityAlertPackageNameCandidates(alert ProviderSecurityAlert) []string {
	candidates := []string{alert.PackageName}
	packageID := strings.TrimSpace(alert.PackageID)
	if strings.HasPrefix(packageID, "pkg:") {
		candidates = append(candidates, packageNameFromPURL(packageID))
	} else {
		candidates = append(candidates, packageNameFromPackageID(packageID))
	}
	return payloadcore.UniqueSortedStrings(candidates)
}

func extractSecurityAlertImpacts(envelopes []facts.Envelope) []SecurityAlertImpact {
	impacts := make([]SecurityAlertImpact, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.ReducerSupplyChainImpactFindingFactKind {
			continue
		}
		impacts = append(impacts, SecurityAlertImpact{
			FactID:       envelope.FactID,
			RepositoryID: payloadcore.PayloadStr(envelope.Payload, "repository_id"),
			PackageID:    payloadcore.PayloadStr(envelope.Payload, "package_id"),
			CVEID:        payloadcore.PayloadStr(envelope.Payload, "cve_id"),
			AdvisoryID:   payloadcore.PayloadStr(envelope.Payload, "advisory_id"),
			Status:       payloadcore.PayloadStr(envelope.Payload, "impact_status"),
		})
	}
	return impacts
}

func classifyProviderSecurityAlert(
	alert ProviderSecurityAlert,
	consumptions []SecurityAlertConsumption,
	impacts []SecurityAlertImpact,
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

	exactConsumption, staleConsumption, ambiguousConsumption := MatchSecurityAlertConsumption(alert, consumptions)
	if exactConsumption.FactID == "" {
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
		if staleConsumption.FactID != "" {
			decision.RepositoryID = staleConsumption.RepositoryID
			decision.RepositoryName = payloadcore.FirstNonBlank(staleConsumption.RepositoryName, decision.RepositoryName)
			decision.Status = SecurityAlertReconciliationStale
			applySecurityAlertDependencyEvidence(&decision, alert, staleConsumption)
			decision.EvidenceFactIDs = payloadcore.UniqueSortedStrings(append(decision.EvidenceFactIDs, staleConsumption.FactID))
			decision.Reason = "newer owned dependency evidence no longer matches the provider alert manifest path"
			decision.ReasonCode = securityAlertReasonProviderAlertStale
			decision.MissingEvidence = securityAlertMissingEvidence(
				"current_manifest",
				"provider_manifest_no_longer_observed",
				staleConsumption.FactID,
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
	decision.RepositoryID = exactConsumption.RepositoryID
	decision.RepositoryName = payloadcore.FirstNonBlank(exactConsumption.RepositoryName, decision.RepositoryName)
	applySecurityAlertDependencyEvidence(&decision, alert, exactConsumption)
	decision.EvidenceFactIDs = payloadcore.UniqueSortedStrings(append(decision.EvidenceFactIDs, exactConsumption.FactID))
	if status, reasonCode, missing, ok := securityAlertUnsupportedTriage(alert); ok {
		decision.Status = status
		decision.Reason = "provider alert ecosystem is unsupported by the current Eshu impact matcher"
		decision.ReasonCode = reasonCode
		decision.MissingEvidence = missing
		return decision
	}

	alert.RepositoryID = exactConsumption.RepositoryID
	impact := matchSecurityAlertImpact(alert, impacts)
	if impact.FactID == "" {
		decision.Status = SecurityAlertReconciliationUnmatched
		decision.Reason = "owned dependency exists but no reducer impact finding matches the provider advisory identifiers"
		decision.ReasonCode = securityAlertReasonImpactFindingMissing
		decision.MissingEvidence = securityAlertMissingEvidence(
			"impact_finding",
			"no_matching_impact_finding",
			exactConsumption.FactID,
		)
		return decision
	}
	decision.Status = SecurityAlertReconciliationMatched
	decision.EshuImpactStatus = impact.Status
	decision.EshuImpactFindingID = impact.FactID
	decision.ImpactEvidenceID = impact.FactID
	decision.EvidenceFactIDs = payloadcore.UniqueSortedStrings(append(decision.EvidenceFactIDs, impact.FactID))
	decision.Reason = "provider alert matches owned dependency and reducer impact evidence"
	decision.ReasonCode = securityAlertReasonMatchedExactImpact
	return decision
}

// securityAlertPayloadBoolPointer is the reducer root's former
// payloadBoolPointer (supply_chain_impact_match.go), a thin nil-guard over
// [payloadcore.PayloadBoolPointerValue]. This family was its only caller on
// main, so the #6061 move left the root copy unused and this PR deletes it
// there; the body lives here now rather than being imported back from root.
func securityAlertPayloadBoolPointer(payload map[string]any, key string) *bool {
	value, ok := payloadcore.PayloadBoolPointerValue(payload, key)
	if !ok {
		return nil
	}
	return &value
}

// securityAlertDependencyScope is the reducer root's former
// supplyChainDependencyScope (supply_chain_impact_match.go). Same history as
// [securityAlertPayloadBoolPointer] above: this family was its only caller, so
// this PR deletes the root copy rather than leaving it dead.
func securityAlertDependencyScope(payload map[string]any) string {
	if scope := payloadcore.PayloadStr(payload, "dependency_scope"); scope != "" {
		return scope
	}
	return payloadcore.PayloadStr(payload, "manifest_section")
}

// packageNameFromPURL and packageNameFromPackageID are declared locally
// rather than imported from the reducer root
// (supply_chain_impact_manifest_dependency.go): they are pure syntactic
// parsers (purl path extraction, Eshu package-registry URI path extraction)
// with no dependency on any reducer-root state, shared verbatim with
// supply_chain's own package-name-candidate matching, which has not moved
// out of root yet (issue #6061).
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
