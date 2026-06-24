// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// IncidentRoutingEvidenceInput is one reducer-ready incident-routing evidence
// packet. Storage adapters build it from incident.record facts, Terraform-source
// PagerDutyDeclaration content rows, Terraform-state routing facts, and optional
// live PagerDuty routing facts.
type IncidentRoutingEvidenceInput struct {
	Incident IncidentRoutingIncident
	Declared []IncidentRoutingDeclaredEvidence
	Applied  []IncidentRoutingAppliedEvidence
	Observed []IncidentRoutingObservedEvidence
	Warnings []IncidentRoutingCoverageWarning
}

// IncidentRoutingIncident is the provider-reported incident anchor used to
// decide whether routing evidence matches an incident service.
type IncidentRoutingIncident struct {
	Provider           string
	ProviderIncidentID string
	ScopeID            string
	ServiceID          string
	ServiceName        string
	ServiceURL         string
	EvidenceFactID     string
	SourceURL          string
	SourceConfidence   string
	ObservedAt         string
}

// IncidentRoutingDeclaredEvidence is Terraform-source intended PagerDuty
// routing evidence from a PagerDutyDeclaration content entity.
type IncidentRoutingDeclaredEvidence struct {
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

// IncidentRoutingAppliedEvidence is Terraform-state applied PagerDuty routing
// evidence from incident_routing.applied_pagerduty_resource facts.
type IncidentRoutingAppliedEvidence struct {
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

// IncidentRoutingObservedEvidence is live PagerDuty routing evidence from
// incident_routing.observed_pagerduty_service facts.
type IncidentRoutingObservedEvidence struct {
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

// IncidentRoutingCoverageWarning is bounded routing coverage warning evidence
// emitted by collectors when live routing cannot be fully observed.
type IncidentRoutingCoverageWarning struct {
	FactID           string
	SourceClass      string
	SourceKind       string
	Reason           string
	ResourceClass    string
	ProviderObjectID string
	ObservedAt       string
}

type incidentRoutingProjectionTally struct {
	materialized map[string]int
	skipped      map[string]int
}

func newIncidentRoutingProjectionTally() incidentRoutingProjectionTally {
	return incidentRoutingProjectionTally{
		materialized: make(map[string]int),
		skipped:      make(map[string]int),
	}
}
