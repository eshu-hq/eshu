// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// AppliedPagerDutyResource is the schema-version-1 typed payload for the
// "incident_routing.applied_pagerduty_resource" fact kind: one PagerDuty
// routing resource observed from applied Terraform state.
//
// The required set matches the collector emitter
// (terraformstate.emitAppliedPagerDutyResource over incidentRoutingBasePayload),
// which unconditionally emits the routing/source classification, the Terraform
// state locator, and the backend join key on every applied resource, and always
// sets ResourceClass on the applied-resource path. ProviderObjectID and
// NameFingerprint are OPTIONAL because the emitter sets them only when the state
// attribute is present: a name-only routing resource omits the provider id, and
// the incident-repository correlation loader deliberately consumes a blank
// provider id as a provenance-only rejected decision rather than dead-lettering
// it.
//
// BackendKind, LocatorHash, ResourceClass, SourceClass, SourceKind, Outcome,
// and StateGenerationID are additionally the fields the two raw-SQL-JSONB
// loaders (incident_repository_correlation_loader.go and
// service_incident_evidence_loader.go) read; those loaders are outside the
// factschema decode seam and so outside the #4573 payload-usage manifest gate's
// view (their conversion is tracked in #4683), so declaring them here is what
// keeps a future dropped field a visible schema-diff break instead of a silent
// read of a missing column.
// TestIncidentRoutingSQLProjectedFieldsAreSchemaDeclared locks that coverage.
type AppliedPagerDutyResource struct {
	// SourceClass is the routing source class ("applied"). Required — the base
	// payload always emits it.
	SourceClass string `json:"source_class"`

	// SourceKind is the routing source kind ("terraform_state"). Required.
	SourceKind string `json:"source_kind"`

	// Outcome is the applied-routing outcome token ("applied"). Required.
	Outcome string `json:"outcome"`

	// ResourceClass is the normalized PagerDuty resource class ("service",
	// "escalation_policy", ...). Required — the applied-resource emitter always
	// sets it, and the incident-repository correlation query filters on
	// resource_class = 'service' to select edge-anchorable rows.
	ResourceClass string `json:"resource_class"`

	// TerraformStateAddress is the resource's Terraform state address. Required.
	TerraformStateAddress string `json:"terraform_state_address"`

	// ResourceType is the Terraform resource type. Required.
	ResourceType string `json:"resource_type"`

	// ResourceName is the Terraform resource name. Required.
	ResourceName string `json:"resource_name"`

	// ModuleAddress is the Terraform module address. Required — always emitted
	// (may be the empty string for a root-module resource).
	ModuleAddress string `json:"module_address"`

	// ProviderAddress is the Terraform provider address. Required.
	ProviderAddress string `json:"provider_address"`

	// ScopeID is the ingestion scope the state was applied under. Required.
	ScopeID string `json:"scope_id"`

	// StateGenerationID is the state's generation id. Required — the base
	// payload always emits it, and the service-incident evidence read model
	// treats it as per-run metadata excluded from durable identity.
	StateGenerationID string `json:"state_generation_id"`

	// StateLineage is the Terraform state lineage. Required.
	StateLineage string `json:"state_lineage"`

	// BackendKind is the Terraform backend kind (s3, gcs, azurerm, ...).
	// Required — it is half of the durable backend-locator repository join key
	// the incident-repository correlation reducer resolves.
	BackendKind string `json:"backend_kind"`

	// LocatorHash is the version-agnostic backend locator hash. Required — it is
	// the other half of the durable repository join key.
	LocatorHash string `json:"locator_hash"`

	// DeclaredMatchState is the declared-vs-applied match state
	// ("not_compared"). Required — the base payload always emits it.
	DeclaredMatchState string `json:"declared_match_state"`

	// RedactionState is the payload redaction state. Required — the base payload
	// always emits it.
	RedactionState string `json:"redaction_state"`

	// StateSerial is the Terraform state serial. Optional: a numeric provenance
	// value that may be absent from a state with no serial.
	StateSerial *int64 `json:"state_serial,omitempty"`

	// ProviderObjectID is the real PagerDuty provider object id. Optional: the
	// emitter sets it only when the state attribute "id" is present; a name-only
	// routing resource omits it, and the correlation loader consumes a blank id
	// as provenance-only rejected routing.
	ProviderObjectID *string `json:"provider_object_id,omitempty"`

	// NameFingerprint is the redaction-safe fingerprint of the resource name.
	// Optional: the emitter sets it only when the state attribute "name" is
	// present.
	NameFingerprint *string `json:"name_fingerprint,omitempty"`

	// EscalationPolicyReference is the referenced escalation policy id. Optional:
	// emitted only for resources that carry one.
	EscalationPolicyReference *string `json:"escalation_policy_reference,omitempty"`

	// ServiceReference is the referenced service id. Optional.
	ServiceReference *string `json:"service_reference,omitempty"`

	// IntegrationType is the service-integration type. Optional.
	IntegrationType *string `json:"integration_type,omitempty"`
}

// AppliedAlertRoute is the schema-version-1 typed payload for the
// "incident_routing.applied_alert_route" fact kind: one alert route resource
// (an AWS EventBridge rule, SNS topic, Lambda, ...) observed from applied
// Terraform state that routes to PagerDuty.
//
// The required set matches the emitter (terraformstate.emitAppliedAlertRoute
// over incidentRoutingBasePayload), which unconditionally emits the same base
// routing classification and locator as the applied PagerDuty resource plus the
// RouteType discriminator. Every target-reference field is optional: the emitter
// sets them only for the alert-route resources that carry the matching attribute
// (an ARN, an endpoint, a redacted value). This kind has no reducer decode call
// site today; the struct exists so the kind carries a versioned contract.
type AppliedAlertRoute struct {
	// SourceClass is the routing source class ("applied"). Required.
	SourceClass string `json:"source_class"`

	// SourceKind is the routing source kind ("terraform_state"). Required.
	SourceKind string `json:"source_kind"`

	// Outcome is the applied-routing outcome token ("applied"). Required.
	Outcome string `json:"outcome"`

	// TerraformStateAddress is the resource's Terraform state address. Required.
	TerraformStateAddress string `json:"terraform_state_address"`

	// ResourceType is the Terraform resource type. Required.
	ResourceType string `json:"resource_type"`

	// ResourceName is the Terraform resource name. Required.
	ResourceName string `json:"resource_name"`

	// ModuleAddress is the Terraform module address. Required.
	ModuleAddress string `json:"module_address"`

	// ProviderAddress is the Terraform provider address. Required.
	ProviderAddress string `json:"provider_address"`

	// ScopeID is the ingestion scope. Required.
	ScopeID string `json:"scope_id"`

	// StateGenerationID is the state's generation id. Required.
	StateGenerationID string `json:"state_generation_id"`

	// StateLineage is the Terraform state lineage. Required.
	StateLineage string `json:"state_lineage"`

	// BackendKind is the Terraform backend kind. Required.
	BackendKind string `json:"backend_kind"`

	// LocatorHash is the backend locator hash. Required.
	LocatorHash string `json:"locator_hash"`

	// DeclaredMatchState is the declared-vs-applied match state. Required.
	DeclaredMatchState string `json:"declared_match_state"`

	// RedactionState is the payload redaction state. Required.
	RedactionState string `json:"redaction_state"`

	// RouteType is the alert-route resource type token (event_rule, sns_topic,
	// lambda_function, ...). Required — the emitter always sets it on this path.
	RouteType string `json:"route_type"`

	// StateSerial is the Terraform state serial. Optional.
	StateSerial *int64 `json:"state_serial,omitempty"`

	// AWSARN is the route resource's AWS ARN. Optional: emitted only when the
	// state carries an arn:-prefixed value.
	AWSARN *string `json:"aws_arn,omitempty"`

	// TargetReferenceKind classifies a redacted PagerDuty target reference.
	// Optional.
	TargetReferenceKind *string `json:"target_reference_kind,omitempty"`

	// TargetReferenceFingerprint is the redaction-safe fingerprint of a target
	// reference. Optional.
	TargetReferenceFingerprint *string `json:"target_reference_fingerprint,omitempty"`
}

// ObservedPagerDutyService is the schema-version-1 typed payload for the
// "incident_routing.observed_pagerduty_service" fact kind: one live PagerDuty
// service observed from the PagerDuty REST API.
//
// The required set matches the emitter (pagerduty.NewObservedPagerDutyServiceEnvelope
// over observedConfigBasePayload), which unconditionally emits the observed
// classification, the resource class, the provider object id, the scope, the
// declared match state, and (via setRedactionState) the redaction state, and
// always sets ServiceID. ProviderObjectID is required by the base payload but
// consumers fall back to ServiceID; both are present, so ProviderObjectID stays
// required (the emitter always sets it) while the reducer's decode-side mapping
// keeps the ServiceID fallback. All observable properties (status, timestamps,
// booleans, drift reason) are optional — the emitter sets them only when
// present.
type ObservedPagerDutyService struct {
	// Provider is the incident provider token. Required.
	Provider string `json:"provider"`

	// SourceClass is the routing source class ("observed"). Required.
	SourceClass string `json:"source_class"`

	// SourceKind is the routing source kind (the PagerDuty API). Required.
	SourceKind string `json:"source_kind"`

	// Outcome is the observed-routing outcome token ("observed"). Required.
	Outcome string `json:"outcome"`

	// ResourceClass is the observed resource class ("service"). Required.
	ResourceClass string `json:"resource_class"`

	// ProviderObjectID is the live service's provider id. Required — the base
	// payload always emits it. Consumers may fall back to ServiceID, but the
	// field itself is unconditionally present.
	ProviderObjectID string `json:"provider_object_id"`

	// ScopeID is the ingestion scope. Required.
	ScopeID string `json:"scope_id"`

	// DeclaredMatchState is the declared-vs-observed match state. Required.
	DeclaredMatchState string `json:"declared_match_state"`

	// RedactionState is the payload redaction state. Required — setRedactionState
	// always sets it.
	RedactionState string `json:"redaction_state"`

	// ServiceID is the live service id. Required — the emitter always sets it
	// after the base payload.
	ServiceID string `json:"service_id"`

	// Status is the live service status. Optional.
	Status *string `json:"status,omitempty"`

	// AlertCreation is the service's alert-creation mode. Optional.
	AlertCreation *string `json:"alert_creation,omitempty"`

	// EscalationPolicyReference is the referenced escalation policy id. Optional.
	EscalationPolicyReference *string `json:"escalation_policy_reference,omitempty"`

	// TeamReferences are the referenced team ids. Optional.
	TeamReferences []string `json:"team_references,omitempty"`

	// NameFingerprint is the redaction-safe fingerprint of the service summary.
	// Optional.
	NameFingerprint *string `json:"name_fingerprint,omitempty"`

	// CreatedAt is the service creation timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the service last-update timestamp (RFC 3339). Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// Disabled reports the service is disabled. Optional: the emitter sets it
	// only when true, so nil stays distinct from an observed false.
	Disabled *bool `json:"disabled,omitempty"`

	// Deleted reports the service was deleted. Optional.
	Deleted *bool `json:"deleted,omitempty"`

	// ManuallyCreated reports the service was created outside Terraform.
	// Optional.
	ManuallyCreated *bool `json:"manually_created,omitempty"`

	// DriftCandidateReason records why the service is a drift candidate.
	// Optional.
	DriftCandidateReason *string `json:"drift_candidate_reason,omitempty"`

	// SourceURL is the provider's service URL. Optional.
	SourceURL *string `json:"source_url,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// ObservedPagerDutyIntegration is the schema-version-1 typed payload for the
// "incident_routing.observed_pagerduty_integration" fact kind: one live
// PagerDuty service integration observed from the PagerDuty REST API.
//
// The required set matches the emitter
// (pagerduty.NewObservedPagerDutyIntegrationEnvelope over
// observedConfigBasePayload), which unconditionally emits the same observed
// classification base as the service plus IntegrationID. This kind has no
// reducer decode call site today; the struct exists so the kind carries a
// versioned contract.
type ObservedPagerDutyIntegration struct {
	// Provider is the incident provider token. Required.
	Provider string `json:"provider"`

	// SourceClass is the routing source class ("observed"). Required.
	SourceClass string `json:"source_class"`

	// SourceKind is the routing source kind (the PagerDuty API). Required.
	SourceKind string `json:"source_kind"`

	// Outcome is the observed-routing outcome token ("observed"). Required.
	Outcome string `json:"outcome"`

	// ResourceClass is the observed resource class ("service_integration").
	// Required.
	ResourceClass string `json:"resource_class"`

	// ProviderObjectID is the live integration's provider id. Required — the
	// base payload always emits it.
	ProviderObjectID string `json:"provider_object_id"`

	// ScopeID is the ingestion scope. Required.
	ScopeID string `json:"scope_id"`

	// DeclaredMatchState is the declared-vs-observed match state. Required.
	DeclaredMatchState string `json:"declared_match_state"`

	// RedactionState is the payload redaction state. Required.
	RedactionState string `json:"redaction_state"`

	// IntegrationID is the live integration id. Required — the emitter always
	// sets it after the base payload.
	IntegrationID string `json:"integration_id"`

	// ServiceReference is the parent service id. Optional.
	ServiceReference *string `json:"service_reference,omitempty"`

	// IntegrationType is the integration type token. Optional.
	IntegrationType *string `json:"integration_type,omitempty"`

	// VendorReference is the integration vendor id. Optional.
	VendorReference *string `json:"vendor_reference,omitempty"`

	// RoutingKeyRedacted reports a redacted routing key was present. Optional.
	RoutingKeyRedacted *bool `json:"routing_key_redacted,omitempty"`

	// NameFingerprint is the redaction-safe fingerprint of the integration
	// summary. Optional.
	NameFingerprint *string `json:"name_fingerprint,omitempty"`

	// CreatedAt is the integration creation timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the integration last-update timestamp (RFC 3339). Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// Disabled reports the integration is disabled. Optional.
	Disabled *bool `json:"disabled,omitempty"`

	// Deleted reports the integration was deleted. Optional.
	Deleted *bool `json:"deleted,omitempty"`

	// ManuallyCreated reports the integration was created outside Terraform.
	// Optional.
	ManuallyCreated *bool `json:"manually_created,omitempty"`

	// DriftCandidateReason records why the integration is a drift candidate.
	// Optional.
	DriftCandidateReason *string `json:"drift_candidate_reason,omitempty"`

	// SourceURL is the provider's integration URL. Optional.
	SourceURL *string `json:"source_url,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// CoverageWarning is the schema-version-1 typed payload for the
// "incident_routing.coverage_warning" fact kind: one bounded coverage gap
// emitted while collecting incident-routing evidence, from either the
// Terraform-state collector (unsupported PagerDuty resource) or the live
// PagerDuty collector (partial coverage).
//
// Two emitters produce this kind with different base payloads: the
// Terraform-state emitter (terraformstate.emitIncidentRoutingCoverageWarning
// over incidentRoutingBasePayload) and the live emitter
// (pagerduty.NewPagerDutyConfigCoverageWarningEnvelope over
// observedConfigBasePayload). The required set is the INTERSECTION of what BOTH
// unconditionally emit: SourceClass, SourceKind, Outcome, ScopeID, Reason,
// RedactionState, and DeclaredMatchState. Everything else is optional because at
// least one emitter omits it:
//
//   - ResourceClass is set ONLY by the live emitter's observedConfigBasePayload;
//     the Terraform-state emitter builds its payload from
//     incidentRoutingBasePayload, which never sets resource_class. Making it
//     required would dead-letter (quarantine) every Terraform-state
//     coverage_warning, silently dropping real coverage evidence — the accuracy
//     bug this field-shape fix corrects. The reducer's coverage-warning mapper
//     already tolerates a blank resource_class.
//   - Provider and ProviderObjectID: live emitter only.
//   - The state-locator fields (TerraformStateAddress, ResourceType, ...):
//     Terraform-state emitter only.
//
// The reducer reads Reason, SourceClass, SourceKind, and ResourceClass through
// the incident-routing evidence loader; a blank ResourceClass is a valid mapped
// value, not an identity input.
type CoverageWarning struct {
	// SourceClass is the routing source class ("applied" or "observed").
	// Required — both emitters set it.
	SourceClass string `json:"source_class"`

	// SourceKind is the routing source kind. Required — both emitters set it.
	SourceKind string `json:"source_kind"`

	// Outcome is the coverage-warning outcome ("unsupported" or "partial").
	// Required — both emitters set it.
	Outcome string `json:"outcome"`

	// ScopeID is the ingestion scope. Required — both emitters set it.
	ScopeID string `json:"scope_id"`

	// Reason is the coverage-warning reason. Required — both emitters set it.
	Reason string `json:"reason"`

	// RedactionState is the payload redaction state. Required — both emitters
	// set it.
	RedactionState string `json:"redaction_state"`

	// DeclaredMatchState is the declared-vs-observed match state. Required —
	// both bases emit it: the Terraform-state base as "not_compared" and the
	// live base from its match-state argument (defaulting to not_compared).
	DeclaredMatchState string `json:"declared_match_state"`

	// ResourceClass is the warned-about resource class. Optional: ONLY the live
	// (PagerDuty API) emitter sets it (defaulting to "unknown"); the
	// Terraform-state emitter's incidentRoutingBasePayload never sets it, so a
	// Terraform-state coverage_warning carries no resource_class. A required
	// field here would dead-letter every such fact.
	ResourceClass *string `json:"resource_class,omitempty"`

	// Provider is the incident provider token. Optional: only the live
	// (PagerDuty API) emitter sets it; the Terraform-state emitter does not.
	Provider *string `json:"provider,omitempty"`

	// ProviderObjectID is the warned-about resource's provider id. Optional:
	// only the live emitter sets it (from the base payload).
	ProviderObjectID *string `json:"provider_object_id,omitempty"`

	// TerraformStateAddress is the Terraform state address. Optional: only the
	// Terraform-state emitter sets it.
	TerraformStateAddress *string `json:"terraform_state_address,omitempty"`

	// ResourceType is the Terraform resource type. Optional: Terraform-state
	// emitter only.
	ResourceType *string `json:"resource_type,omitempty"`

	// ResourceName is the Terraform resource name. Optional: Terraform-state
	// emitter only.
	ResourceName *string `json:"resource_name,omitempty"`

	// ModuleAddress is the Terraform module address. Optional: Terraform-state
	// emitter only.
	ModuleAddress *string `json:"module_address,omitempty"`

	// ProviderAddress is the Terraform provider address. Optional:
	// Terraform-state emitter only.
	ProviderAddress *string `json:"provider_address,omitempty"`

	// StateGenerationID is the Terraform state generation id. Optional:
	// Terraform-state emitter only.
	StateGenerationID *string `json:"state_generation_id,omitempty"`

	// StateLineage is the Terraform state lineage. Optional: Terraform-state
	// emitter only.
	StateLineage *string `json:"state_lineage,omitempty"`

	// StateSerial is the Terraform state serial. Optional: Terraform-state
	// emitter only.
	StateSerial *int64 `json:"state_serial,omitempty"`

	// BackendKind is the Terraform backend kind. Optional: Terraform-state
	// emitter only.
	BackendKind *string `json:"backend_kind,omitempty"`

	// LocatorHash is the backend locator hash. Optional: Terraform-state emitter
	// only.
	LocatorHash *string `json:"locator_hash,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional: only the
	// live emitter sets it.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}
