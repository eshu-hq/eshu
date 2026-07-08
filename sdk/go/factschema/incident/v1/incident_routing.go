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

	// AlertCreation is the PagerDuty service alert-creation mode. Optional.
	AlertCreation *string `json:"alert_creation,omitempty"`

	// DeliveryMethod is the PagerDuty integration delivery method. Optional.
	DeliveryMethod *string `json:"delivery_method,omitempty"`

	// WebhookObjectReference is a redaction-safe referenced webhook object id.
	// Optional.
	WebhookObjectReference *string `json:"webhook_object_reference,omitempty"`

	// WebhookObjectType is the referenced webhook object type. Optional.
	WebhookObjectType *string `json:"webhook_object_type,omitempty"`

	// RedactedAttributes lists Terraform attributes whose presence was
	// redacted. Optional.
	RedactedAttributes *string `json:"redacted_attributes,omitempty"`

	// ConfigRedacted reports a redacted config attribute was present. Optional.
	ConfigRedacted *bool `json:"config_redacted,omitempty"`

	// EmailRedacted reports a redacted email attribute was present. Optional.
	EmailRedacted *bool `json:"email_redacted,omitempty"`

	// HTMLURLRedacted reports a redacted html_url attribute was present.
	// Optional.
	HTMLURLRedacted *bool `json:"html_url_redacted,omitempty"`

	// IntegrationKeyRedacted reports a redacted integration key was present.
	// Optional.
	IntegrationKeyRedacted *bool `json:"integration_key_redacted,omitempty"`

	// PrivateURLRedacted reports a redacted private_url attribute was present.
	// Optional.
	PrivateURLRedacted *bool `json:"private_url_redacted,omitempty"`

	// RoutingKeyRedacted reports a redacted routing key was present. Optional.
	RoutingKeyRedacted *bool `json:"routing_key_redacted,omitempty"`

	// SecretRedacted reports a redacted secret attribute was present. Optional.
	SecretRedacted *bool `json:"secret_redacted,omitempty"`

	// URLRedacted reports a redacted url attribute was present. Optional.
	URLRedacted *bool `json:"url_redacted,omitempty"`

	// WebhookSecretRedacted reports a redacted webhook secret was present.
	// Optional.
	WebhookSecretRedacted *bool `json:"webhook_secret_redacted,omitempty"`
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

	// NameFingerprint is a redaction-safe resource name fingerprint. Optional.
	NameFingerprint *string `json:"name_fingerprint,omitempty"`

	// FunctionNameFingerprint is a redaction-safe function-name fingerprint.
	// Optional.
	FunctionNameFingerprint *string `json:"function_name_fingerprint,omitempty"`

	// TargetIDFingerprint is a redaction-safe target-id fingerprint. Optional.
	TargetIDFingerprint *string `json:"target_id_fingerprint,omitempty"`

	// RuleFingerprint is a redaction-safe rule fingerprint. Optional.
	RuleFingerprint *string `json:"rule_fingerprint,omitempty"`

	// EndpointRedacted reports a redacted endpoint attribute was present.
	// Optional.
	EndpointRedacted *bool `json:"endpoint_redacted,omitempty"`

	// ValueRedacted reports a redacted value attribute was present. Optional.
	ValueRedacted *bool `json:"value_redacted,omitempty"`

	// PolicyRedacted reports a redacted policy attribute was present. Optional.
	PolicyRedacted *bool `json:"policy_redacted,omitempty"`
}
