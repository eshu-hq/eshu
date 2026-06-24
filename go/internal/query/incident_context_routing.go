// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

type incidentRoutingEvidenceInput struct {
	Incident IncidentContextIncident
	Declared []incidentDeclaredPagerDutyRouting
	Applied  []incidentAppliedPagerDutyRouting
	Observed []incidentObservedPagerDutyRouting
	Warnings []incidentRoutingCoverageWarning
}

type incidentDeclaredPagerDutyRouting struct {
	EntityID              string
	RepoID                string
	RelativePath          string
	EntityName            string
	DeclarationKind       string
	SourceClass           string
	Outcome               string
	ServiceName           string
	ServiceNameResolution string
	EscalationPolicy      string
	Environment           string
	Workspace             string
	RedactionState        string
	UnsupportedReason     string
	DuplicateServiceName  bool
	StartLine             int
}

type incidentAppliedPagerDutyRouting struct {
	FactID                    string
	SourceClass               string
	SourceKind                string
	Outcome                   string
	ResourceClass             string
	ProviderObjectID          string
	NameFingerprint           string
	EscalationPolicyReference string
	TerraformStateAddress     string
	ProviderAddress           string
	ModuleAddress             string
	StateGenerationID         string
	DeclaredMatchState        string
	RedactionState            string
	ObservedAt                string
}

type incidentObservedPagerDutyRouting struct {
	FactID                    string
	SourceClass               string
	SourceKind                string
	Outcome                   string
	ServiceID                 string
	ProviderObjectID          string
	NameFingerprint           string
	Status                    string
	EscalationPolicyReference string
	DeclaredMatchState        string
	DriftCandidateReason      string
	RedactionState            string
	SourceURL                 string
	Disabled                  bool
	Deleted                   bool
	ManuallyCreated           bool
	ObservedAt                string
}

type incidentRoutingCoverageWarning struct {
	FactID           string
	SourceClass      string
	SourceKind       string
	Reason           string
	ResourceClass    string
	ProviderObjectID string
	ObservedAt       string
}

func buildIncidentRoutingEvidence(
	input incidentRoutingEvidenceInput,
) []IncidentContextEvidenceEdge {
	appliedEdge, selectedApplied := buildIncidentAppliedRoutingEdge(input.Incident, input.Applied)
	return []IncidentContextEvidenceEdge{
		buildIncidentDeclaredRoutingEdge(input.Incident, input.Declared),
		appliedEdge,
		buildIncidentObservedRoutingEdge(input.Incident, input.Observed, selectedApplied, input.Warnings),
	}
}

func buildIncidentDeclaredRoutingEdge(
	incident IncidentContextIncident,
	declared []incidentDeclaredPagerDutyRouting,
) IncidentContextEvidenceEdge {
	candidates := incidentDeclaredRoutingCandidates(incident, declared)
	if len(candidates) == 0 {
		return missingIncidentContextEdge(IncidentSlotIntendedRouting)
	}
	if len(candidates) > 1 || declaredRoutingAmbiguous(candidates[0]) {
		return IncidentContextEvidenceEdge{
			Slot:        IncidentSlotIntendedRouting,
			TruthLabel:  IncidentTruthAmbiguous,
			Explanation: "multiple Terraform-declared PagerDuty service declarations match the incident service",
			Candidates:  incidentDeclaredRoutingCandidateValues(candidates),
		}
	}
	item := candidates[0]
	label := IncidentTruthExact
	if strings.TrimSpace(item.Outcome) == "rejected" || strings.TrimSpace(item.Outcome) == "unsupported" {
		label = IncidentTruthRejected
	} else if strings.TrimSpace(item.ServiceNameResolution) != "" &&
		strings.TrimSpace(item.ServiceNameResolution) != "literal" {
		label = IncidentTruthDerived
	}
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotIntendedRouting,
		TruthLabel:  label,
		Explanation: incidentDeclaredRoutingExplanation(item),
		Value: map[string]string{
			"source_class":      firstNonEmpty(item.SourceClass, "declared"),
			"outcome":           item.Outcome,
			"repo_id":           item.RepoID,
			"relative_path":     item.RelativePath,
			"declaration_kind":  item.DeclarationKind,
			"environment":       item.Environment,
			"workspace":         item.Workspace,
			"redaction_state":   item.RedactionState,
			"service_name_hash": incidentRoutingShortFingerprint(item.ServiceName),
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentContentEvidenceRef("content_entity.PagerDutyDeclaration", item.EntityID, "", "terraform_source"),
		},
	}
}

func buildIncidentAppliedRoutingEdge(
	incident IncidentContextIncident,
	applied []incidentAppliedPagerDutyRouting,
) (IncidentContextEvidenceEdge, *incidentAppliedPagerDutyRouting) {
	candidates := incidentAppliedRoutingCandidates(incident, applied)
	if len(candidates) == 0 {
		return missingIncidentContextEdge(IncidentSlotAppliedRouting), nil
	}
	if len(candidates) > 1 {
		return IncidentContextEvidenceEdge{
			Slot:        IncidentSlotAppliedRouting,
			TruthLabel:  IncidentTruthAmbiguous,
			Explanation: "multiple applied Terraform-state PagerDuty services match the incident service",
			Candidates:  incidentAppliedRoutingCandidateValues(candidates),
		}, nil
	}
	item := candidates[0]
	label := IncidentTruthExact
	if !strings.EqualFold(strings.TrimSpace(item.ProviderObjectID), strings.TrimSpace(incident.Service.ID)) {
		label = IncidentTruthDerived
	}
	if strings.TrimSpace(item.Outcome) == "rejected" {
		label = IncidentTruthRejected
	}
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotAppliedRouting,
		TruthLabel:  label,
		Explanation: incidentAppliedRoutingExplanation(item, label),
		Value: map[string]string{
			"source_class":            firstNonEmpty(item.SourceClass, "applied"),
			"source_kind":             item.SourceKind,
			"outcome":                 item.Outcome,
			"resource_class":          item.ResourceClass,
			"provider_object_id":      item.ProviderObjectID,
			"terraform_state_address": item.TerraformStateAddress,
			"provider_address":        item.ProviderAddress,
			"module_address":          item.ModuleAddress,
			"state_generation_id":     item.StateGenerationID,
			"declared_match_state":    item.DeclaredMatchState,
			"redaction_state":         item.RedactionState,
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentEvidenceRef("incident_routing.applied_pagerduty_resource", item.FactID, "", "terraform_state"),
		},
	}, &item
}

func buildIncidentObservedRoutingEdge(
	incident IncidentContextIncident,
	observed []incidentObservedPagerDutyRouting,
	applied *incidentAppliedPagerDutyRouting,
	warnings []incidentRoutingCoverageWarning,
) IncidentContextEvidenceEdge {
	candidates := incidentObservedRoutingCandidates(incident, observed)
	if len(candidates) == 0 {
		if warning := incidentRoutingBestWarning(warnings); warning != nil {
			return incidentRoutingWarningEdge(*warning)
		}
		return missingIncidentContextEdge(IncidentSlotLiveRouting)
	}
	if len(candidates) > 1 {
		return IncidentContextEvidenceEdge{
			Slot:        IncidentSlotLiveRouting,
			TruthLabel:  IncidentTruthAmbiguous,
			Explanation: "multiple live PagerDuty services match the incident service",
			Candidates:  incidentObservedRoutingCandidateValues(candidates),
		}
	}
	item := candidates[0]
	label := incidentObservedRoutingTruthLabel(incident, item, applied)
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotLiveRouting,
		TruthLabel:  label,
		Explanation: incidentObservedRoutingExplanation(item, applied, label),
		Value: map[string]string{
			"source_class":           firstNonEmpty(item.SourceClass, "observed"),
			"source_kind":            item.SourceKind,
			"outcome":                item.Outcome,
			"service_id":             firstNonEmpty(item.ServiceID, item.ProviderObjectID),
			"status":                 item.Status,
			"declared_match_state":   item.DeclaredMatchState,
			"drift_candidate_reason": item.DriftCandidateReason,
			"redaction_state":        item.RedactionState,
			"disabled":               boolString(item.Disabled),
			"deleted":                boolString(item.Deleted),
			"manually_created":       boolString(item.ManuallyCreated),
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentEvidenceRef("incident_routing.observed_pagerduty_service", item.FactID, item.SourceURL, "pagerduty_api"),
		},
	}
}

func incidentObservedRoutingTruthLabel(
	incident IncidentContextIncident,
	observed incidentObservedPagerDutyRouting,
	applied *incidentAppliedPagerDutyRouting,
) IncidentTruthLabel {
	if strings.TrimSpace(observed.Outcome) == "rejected" {
		return IncidentTruthRejected
	}
	if observed.Deleted {
		return IncidentTruthStale
	}
	if applied != nil &&
		strings.TrimSpace(applied.EscalationPolicyReference) != "" &&
		strings.TrimSpace(observed.EscalationPolicyReference) != "" &&
		strings.TrimSpace(applied.EscalationPolicyReference) != strings.TrimSpace(observed.EscalationPolicyReference) {
		return IncidentTruthDrifted
	}
	if strings.TrimSpace(observed.DeclaredMatchState) == "drifted" {
		return IncidentTruthDrifted
	}
	if !strings.EqualFold(strings.TrimSpace(observed.ServiceID), strings.TrimSpace(incident.Service.ID)) &&
		!strings.EqualFold(strings.TrimSpace(observed.ProviderObjectID), strings.TrimSpace(incident.Service.ID)) {
		return IncidentTruthDerived
	}
	return IncidentTruthExact
}

func incidentRoutingBestWarning(warnings []incidentRoutingCoverageWarning) *incidentRoutingCoverageWarning {
	for idx := range warnings {
		if incidentRoutingWarningTruthLabel(warnings[idx]) == IncidentTruthPermissionHidden {
			return &warnings[idx]
		}
	}
	if len(warnings) == 0 {
		return nil
	}
	return &warnings[0]
}

func incidentRoutingWarningEdge(warning incidentRoutingCoverageWarning) IncidentContextEvidenceEdge {
	return IncidentContextEvidenceEdge{
		Slot:        IncidentSlotLiveRouting,
		TruthLabel:  incidentRoutingWarningTruthLabel(warning),
		Explanation: incidentRoutingWarningExplanation(warning),
		Value: map[string]string{
			"source_class":       warning.SourceClass,
			"source_kind":        warning.SourceKind,
			"reason":             warning.Reason,
			"resource_class":     warning.ResourceClass,
			"provider_object_id": warning.ProviderObjectID,
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentEvidenceRef("incident_routing.coverage_warning", warning.FactID, "", warning.SourceKind),
		},
	}
}

func incidentRoutingWarningTruthLabel(warning incidentRoutingCoverageWarning) IncidentTruthLabel {
	reason := strings.ToLower(strings.TrimSpace(warning.Reason))
	switch {
	case strings.Contains(reason, "permission"):
		return IncidentTruthPermissionHidden
	case strings.Contains(reason, "stale"):
		return IncidentTruthStale
	case strings.Contains(reason, "reject") || strings.Contains(reason, "unsupported"):
		return IncidentTruthRejected
	default:
		return IncidentTruthUnresolved
	}
}

func declaredRoutingAmbiguous(item incidentDeclaredPagerDutyRouting) bool {
	return item.DuplicateServiceName || strings.TrimSpace(item.Outcome) == string(IncidentTruthAmbiguous)
}

func incidentDeclaredRoutingExplanation(item incidentDeclaredPagerDutyRouting) string {
	switch strings.TrimSpace(item.Outcome) {
	case "rejected":
		return "Terraform PagerDuty declaration matched the incident service but was rejected during parsing"
	case "unsupported":
		return "Terraform PagerDuty declaration matched the incident service but the module source is unsupported"
	default:
		return "Terraform source declares intended PagerDuty routing for the incident service"
	}
}

func incidentAppliedRoutingExplanation(
	item incidentAppliedPagerDutyRouting,
	label IncidentTruthLabel,
) string {
	if label == IncidentTruthDerived {
		return "Terraform state PagerDuty service matched the incident service by sanitized name fingerprint"
	}
	if strings.TrimSpace(item.DeclaredMatchState) == "missing" {
		return "Terraform state contains PagerDuty service evidence without matching declared source evidence"
	}
	return "Terraform state contains applied PagerDuty service evidence for the incident service"
}

func incidentObservedRoutingExplanation(
	item incidentObservedPagerDutyRouting,
	applied *incidentAppliedPagerDutyRouting,
	label IncidentTruthLabel,
) string {
	switch label {
	case IncidentTruthDrifted:
		if applied != nil {
			return "live PagerDuty service evidence differs from applied Terraform-state routing evidence"
		}
		return "live PagerDuty service evidence reports drift against declared or applied routing evidence"
	case IncidentTruthStale:
		return "live PagerDuty service evidence reports a deleted or stale service"
	case IncidentTruthRejected:
		return "live PagerDuty service evidence was rejected before promotion"
	case IncidentTruthDerived:
		return "live PagerDuty service matched the incident service by sanitized name fingerprint"
	default:
		return "live PagerDuty API confirms the incident service routing evidence"
	}
}

func incidentRoutingWarningExplanation(warning incidentRoutingCoverageWarning) string {
	switch incidentRoutingWarningTruthLabel(warning) {
	case IncidentTruthPermissionHidden:
		return "live PagerDuty service configuration is permission-hidden for this incident scope"
	case IncidentTruthStale:
		return "live PagerDuty routing evidence is stale for this incident scope"
	case IncidentTruthRejected:
		return "live PagerDuty routing evidence was rejected or unsupported for this incident scope"
	default:
		return "live PagerDuty routing evidence could not resolve the incident service"
	}
}

func incidentContentEvidenceRef(
	kind string,
	recordID string,
	url string,
	source string,
) IncidentContextEvidenceRef {
	return IncidentContextEvidenceRef{
		RecordID: recordID,
		Kind:     kind,
		URL:      url,
		Source:   source,
	}
}
