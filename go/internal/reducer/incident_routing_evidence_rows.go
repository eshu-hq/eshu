// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	incidentRoutingSlotIntended = "intended_routing"
	incidentRoutingSlotApplied  = "applied_routing"
	incidentRoutingSlotLive     = "live_routing"

	incidentRoutingTruthExact            = "exact"
	incidentRoutingTruthDerived          = "derived"
	incidentRoutingTruthDrifted          = "drifted"
	incidentRoutingTruthAmbiguous        = "ambiguous"
	incidentRoutingTruthUnresolved       = "unresolved"
	incidentRoutingTruthStale            = "stale"
	incidentRoutingTruthRejected         = "rejected"
	incidentRoutingTruthPermissionHidden = "permission_hidden"
	incidentRoutingTruthMissing          = "missing"
)

type incidentRoutingSlotDecision struct {
	slot    string
	outcome string
	row     map[string]any
}

// ExtractIncidentRoutingEvidenceRows projects exact incident-routing evidence
// into deterministic graph rows. The graph contract is intentionally narrower
// than the incident-context read model: only full declared/applied/observed
// exact convergence, or exact live-only no-IaC evidence, is materialized.
// Drifted, stale, permission-hidden, ambiguous, unresolved, rejected, derived,
// and missing evidence stays provenance-only and is counted in the tally.
func ExtractIncidentRoutingEvidenceRows(
	input IncidentRoutingEvidenceInput,
) ([]map[string]any, incidentRoutingProjectionTally) {
	tally := newIncidentRoutingProjectionTally()
	incidentUID := incidentRoutingIncidentUID(input.Incident)
	decisions := []incidentRoutingSlotDecision{
		incidentRoutingDeclaredDecision(input, incidentUID),
		incidentRoutingAppliedDecision(input, incidentUID),
		incidentRoutingObservedDecision(input, incidentUID),
	}

	if !incidentRoutingEvidenceIsGraphEligible(decisions) {
		for _, decision := range decisions {
			tally.skipped[decision.outcome]++
		}
		return nil, tally
	}

	rows := make([]map[string]any, 0, len(decisions))
	for _, decision := range decisions {
		if decision.outcome != incidentRoutingTruthExact || decision.row == nil {
			tally.skipped[decision.outcome]++
			continue
		}
		rows = append(rows, decision.row)
		tally.materialized[incidentRoutingTruthExact]++
	}
	sort.Slice(rows, func(a, b int) bool {
		left := anyToString(rows[a]["slot"]) + ":" + anyToString(rows[a]["uid"])
		right := anyToString(rows[b]["slot"]) + ":" + anyToString(rows[b]["uid"])
		return left < right
	})
	return rows, tally
}

func incidentRoutingEvidenceIsGraphEligible(decisions []incidentRoutingSlotDecision) bool {
	if len(decisions) != 3 {
		return false
	}
	declared := decisions[0].outcome
	applied := decisions[1].outcome
	observed := decisions[2].outcome
	if declared == incidentRoutingTruthExact &&
		applied == incidentRoutingTruthExact &&
		observed == incidentRoutingTruthExact {
		return true
	}
	return declared == incidentRoutingTruthMissing &&
		applied == incidentRoutingTruthMissing &&
		observed == incidentRoutingTruthExact
}

func incidentRoutingDeclaredDecision(
	input IncidentRoutingEvidenceInput,
	incidentUID string,
) incidentRoutingSlotDecision {
	candidates := incidentRoutingDeclaredCandidates(input.Incident, input.Declared)
	switch {
	case len(candidates) == 0:
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotIntended, outcome: incidentRoutingTruthMissing}
	case len(candidates) > 1 || candidates[0].DuplicateServiceName:
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotIntended, outcome: incidentRoutingTruthAmbiguous}
	}
	item := candidates[0]
	outcome := incidentRoutingTruthExact
	switch strings.TrimSpace(item.Outcome) {
	case "rejected", "unsupported":
		outcome = incidentRoutingTruthRejected
	}
	if resolution := strings.TrimSpace(item.ServiceNameResolution); resolution != "" && resolution != "literal" {
		outcome = incidentRoutingTruthDerived
	}
	if outcome != incidentRoutingTruthExact {
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotIntended, outcome: outcome}
	}
	return incidentRoutingSlotDecision{
		slot:    incidentRoutingSlotIntended,
		outcome: incidentRoutingTruthExact,
		row: incidentRoutingBaseRow(input.Incident, incidentUID, incidentRoutingSlotIntended,
			firstNonBlank(item.SourceClass, "declared"), "content_entity.PagerDutyDeclaration", item.EntityID, map[string]any{
				"repo_id":            item.RepoID,
				"relative_path":      item.RelativePath,
				"declaration_kind":   item.DeclarationKind,
				"environment":        item.Environment,
				"workspace":          item.Workspace,
				"redaction_state":    item.RedactionState,
				"service_name_hash":  incidentRoutingShortFingerprint(item.ServiceName),
				"provider_object_id": "",
			}),
	}
}

func incidentRoutingAppliedDecision(
	input IncidentRoutingEvidenceInput,
	incidentUID string,
) incidentRoutingSlotDecision {
	candidates := incidentRoutingAppliedCandidates(input.Incident, input.Applied)
	switch {
	case len(candidates) == 0:
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotApplied, outcome: incidentRoutingTruthMissing}
	case len(candidates) > 1:
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotApplied, outcome: incidentRoutingTruthAmbiguous}
	}
	item := candidates[0]
	outcome := incidentRoutingTruthExact
	if strings.TrimSpace(item.Outcome) == "rejected" {
		outcome = incidentRoutingTruthRejected
	}
	incidentServiceID := strings.TrimSpace(input.Incident.ServiceID)
	if incidentServiceID == "" || !strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), incidentServiceID) {
		outcome = incidentRoutingTruthDerived
	}
	if outcome != incidentRoutingTruthExact {
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotApplied, outcome: outcome}
	}
	return incidentRoutingSlotDecision{
		slot:    incidentRoutingSlotApplied,
		outcome: incidentRoutingTruthExact,
		row: incidentRoutingBaseRow(input.Incident, incidentUID, incidentRoutingSlotApplied,
			firstNonBlank(item.SourceClass, "applied"), "incident_routing.applied_pagerduty_resource", item.FactID, map[string]any{
				"source_kind":             item.SourceKind,
				"resource_class":          item.ResourceClass,
				"provider_object_id":      item.ProviderObjectID,
				"terraform_state_address": item.TerraformStateAddress,
				"provider_address":        item.ProviderAddress,
				"module_address":          item.ModuleAddress,
				"state_generation_id":     item.StateGenerationID,
				"declared_match_state":    item.DeclaredMatchState,
				"redaction_state":         item.RedactionState,
			}),
	}
}

func incidentRoutingObservedDecision(
	input IncidentRoutingEvidenceInput,
	incidentUID string,
) incidentRoutingSlotDecision {
	candidates := incidentRoutingObservedCandidates(input.Incident, input.Observed)
	switch {
	case len(candidates) == 0:
		if warning := incidentRoutingBestCoverageWarning(input.Warnings); warning != nil {
			return incidentRoutingSlotDecision{
				slot:    incidentRoutingSlotLive,
				outcome: incidentRoutingWarningOutcome(*warning),
			}
		}
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotLive, outcome: incidentRoutingTruthMissing}
	case len(candidates) > 1:
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotLive, outcome: incidentRoutingTruthAmbiguous}
	}
	item := candidates[0]
	outcome := incidentRoutingObservedOutcome(input.Incident, item, input.Applied)
	if outcome != incidentRoutingTruthExact {
		return incidentRoutingSlotDecision{slot: incidentRoutingSlotLive, outcome: outcome}
	}
	return incidentRoutingSlotDecision{
		slot:    incidentRoutingSlotLive,
		outcome: incidentRoutingTruthExact,
		row: incidentRoutingBaseRow(input.Incident, incidentUID, incidentRoutingSlotLive,
			firstNonBlank(item.SourceClass, "observed"), "incident_routing.observed_pagerduty_service", item.FactID, map[string]any{
				"source_kind":            item.SourceKind,
				"provider_object_id":     firstNonBlank(item.ServiceID, item.ProviderObjectID),
				"status":                 item.Status,
				"declared_match_state":   item.DeclaredMatchState,
				"drift_candidate_reason": item.DriftCandidateReason,
				"redaction_state":        item.RedactionState,
			}),
	}
}

func incidentRoutingBaseRow(
	incident IncidentRoutingIncident,
	incidentUID string,
	slot string,
	sourceClass string,
	evidenceKind string,
	evidenceID string,
	extra map[string]any,
) map[string]any {
	row := map[string]any{
		"uid":                  incidentRoutingEvidenceUID(incidentUID, slot, sourceClass, evidenceKind, evidenceID),
		"incident_uid":         incidentUID,
		"slot":                 slot,
		"source_class":         sourceClass,
		"truth_label":          incidentRoutingTruthExact,
		"provider":             firstNonBlank(incident.Provider, "pagerduty"),
		"provider_incident_id": incident.ProviderIncidentID,
		"service_id":           incident.ServiceID,
		"service_url":          incident.ServiceURL,
		"service_name_hash":    incidentRoutingShortFingerprint(incident.ServiceName),
		"evidence_kind":        evidenceKind,
		"evidence_id":          evidenceID,
		"incident_fact_id":     incident.EvidenceFactID,
	}
	for key, value := range extra {
		row[key] = value
	}
	return row
}

func incidentRoutingDeclaredCandidates(
	incident IncidentRoutingIncident,
	declared []IncidentRoutingDeclaredEvidence,
) []IncidentRoutingDeclaredEvidence {
	serviceName := strings.TrimSpace(incident.ServiceName)
	if serviceName == "" {
		return nil
	}
	out := make([]IncidentRoutingDeclaredEvidence, 0, len(declared))
	for _, item := range declared {
		if strings.EqualFold(strings.TrimSpace(item.ServiceName), serviceName) {
			out = append(out, item)
		}
	}
	return out
}

func incidentRoutingAppliedCandidates(
	incident IncidentRoutingIncident,
	applied []IncidentRoutingAppliedEvidence,
) []IncidentRoutingAppliedEvidence {
	serviceID := strings.TrimSpace(incident.ServiceID)
	serviceFingerprint := incidentRoutingShortFingerprint(incident.ServiceName)
	out := make([]IncidentRoutingAppliedEvidence, 0, len(applied))
	for _, item := range applied {
		if serviceID != "" && strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), serviceID) {
			out = append(out, item)
			continue
		}
		if serviceFingerprint != "" && strings.TrimSpace(item.NameFingerprint) == serviceFingerprint {
			out = append(out, item)
		}
	}
	return out
}

func incidentRoutingObservedCandidates(
	incident IncidentRoutingIncident,
	observed []IncidentRoutingObservedEvidence,
) []IncidentRoutingObservedEvidence {
	serviceID := strings.TrimSpace(incident.ServiceID)
	serviceFingerprint := incidentRoutingConfigFingerprint(incident.ServiceName)
	out := make([]IncidentRoutingObservedEvidence, 0, len(observed))
	for _, item := range observed {
		if serviceID != "" &&
			(strings.EqualFold(strings.TrimSpace(item.ServiceID), serviceID) ||
				strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), serviceID)) {
			out = append(out, item)
			continue
		}
		if serviceFingerprint != "" && strings.TrimSpace(item.NameFingerprint) == serviceFingerprint {
			out = append(out, item)
		}
	}
	return out
}

func incidentRoutingObservedOutcome(
	incident IncidentRoutingIncident,
	observed IncidentRoutingObservedEvidence,
	applied []IncidentRoutingAppliedEvidence,
) string {
	if strings.TrimSpace(observed.Outcome) == "rejected" {
		return incidentRoutingTruthRejected
	}
	if observed.Deleted {
		return incidentRoutingTruthStale
	}
	incidentServiceID := strings.TrimSpace(incident.ServiceID)
	if incidentServiceID == "" {
		return incidentRoutingTruthDerived
	}
	for _, item := range applied {
		if strings.TrimSpace(item.EscalationPolicyReference) != "" &&
			strings.TrimSpace(observed.EscalationPolicyReference) != "" &&
			strings.TrimSpace(item.EscalationPolicyReference) != strings.TrimSpace(observed.EscalationPolicyReference) {
			return incidentRoutingTruthDrifted
		}
	}
	if strings.TrimSpace(observed.DeclaredMatchState) == "drifted" {
		return incidentRoutingTruthDrifted
	}
	if !strings.EqualFold(strings.TrimSpace(observed.ServiceID), incidentServiceID) &&
		!strings.EqualFold(strings.TrimSpace(observed.ProviderObjectID), incidentServiceID) {
		return incidentRoutingTruthDerived
	}
	return incidentRoutingTruthExact
}

func incidentRoutingBestCoverageWarning(
	warnings []IncidentRoutingCoverageWarning,
) *IncidentRoutingCoverageWarning {
	for idx := range warnings {
		if incidentRoutingWarningOutcome(warnings[idx]) == incidentRoutingTruthPermissionHidden {
			return &warnings[idx]
		}
	}
	if len(warnings) == 0 {
		return nil
	}
	return &warnings[0]
}

func incidentRoutingWarningOutcome(warning IncidentRoutingCoverageWarning) string {
	reason := strings.ToLower(strings.TrimSpace(warning.Reason))
	switch {
	case strings.Contains(reason, "permission"):
		return incidentRoutingTruthPermissionHidden
	case strings.Contains(reason, "stale"):
		return incidentRoutingTruthStale
	case strings.Contains(reason, "reject") || strings.Contains(reason, "unsupported"):
		return incidentRoutingTruthRejected
	default:
		return incidentRoutingTruthUnresolved
	}
}

func incidentRoutingIncidentUID(incident IncidentRoutingIncident) string {
	return facts.StableID("IncidentRoutingEvidence", map[string]any{
		"node_kind":            "incident",
		"scope_id":             incident.ScopeID,
		"provider":             firstNonBlank(incident.Provider, "pagerduty"),
		"provider_incident_id": incident.ProviderIncidentID,
	})
}

func incidentRoutingEvidenceUID(
	incidentUID string,
	slot string,
	sourceClass string,
	evidenceKind string,
	evidenceID string,
) string {
	return facts.StableID("IncidentRoutingEvidence", map[string]any{
		"node_kind":     "routing",
		"incident_uid":  incidentUID,
		"slot":          slot,
		"source_class":  sourceClass,
		"evidence_kind": evidenceKind,
		"evidence_id":   evidenceID,
	})
}

func incidentRoutingShortFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func incidentRoutingConfigFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
