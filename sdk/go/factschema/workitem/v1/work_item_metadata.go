// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// WorkItemProjectMetadata is the schema-version-1 typed payload for the
// "work_item.project_metadata" fact kind: one provider project definition.
//
// The required set matches the collector emitter
// (jira.NewWorkItemProjectMetadataEnvelope), which rejects a payload only when
// BOTH project id and key are blank — either alone satisfies the guard — so
// neither ProjectID nor ProjectKey can be made individually required without
// risking a dead letter for a valid fact that carries only one. Provider is
// always stamped and is the only field the emitter unconditionally sets.
type WorkItemProjectMetadata struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// ProjectID is the provider project id. Optional: the emitter accepts
	// ProjectKey alone as the identity anchor.
	ProjectID *string `json:"project_id,omitempty"`

	// ProjectKey is the provider project key. Optional: the emitter accepts
	// ProjectID alone as the identity anchor.
	ProjectKey *string `json:"project_key,omitempty"`

	// ProjectTypeKey is the provider project type token. Optional.
	ProjectTypeKey *string `json:"project_type_key,omitempty"`

	// ProjectName is always redacted to the empty string by the collector.
	// Optional; see ProjectNamePresent and ProjectNameFingerprint.
	ProjectName *string `json:"project_name,omitempty"`

	// ProjectNamePresent reports whether the source project carried a
	// non-blank name before redaction. Optional.
	ProjectNamePresent *bool `json:"project_name_present,omitempty"`

	// ProjectNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) project name. Optional.
	ProjectNameFingerprint *string `json:"project_name_fingerprint,omitempty"`

	// CategoryID is the provider project category id. Optional.
	CategoryID *string `json:"category_id,omitempty"`

	// CategoryName is always redacted to the empty string by the collector.
	// Optional; see CategoryNamePresent and CategoryNameFingerprint.
	CategoryName *string `json:"category_name,omitempty"`

	// CategoryNamePresent reports whether the source project carried a
	// non-blank category name before redaction. Optional.
	CategoryNamePresent *bool `json:"category_name_present,omitempty"`

	// CategoryNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) category name. Optional.
	CategoryNameFingerprint *string `json:"category_name_fingerprint,omitempty"`

	// Style is the provider project style token (for example "classic").
	// Optional.
	Style *string `json:"style,omitempty"`

	// VisibilityState classifies the project as "active", "archived", or
	// "deleted". Optional.
	VisibilityState *string `json:"visibility_state,omitempty"`

	// SelfURL is always redacted to the empty string by the collector.
	// Optional; see SelfURLFingerprint.
	SelfURL *string `json:"self_url,omitempty"`

	// SelfURLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// self URL. Optional.
	SelfURLFingerprint *string `json:"self_url_fingerprint,omitempty"`

	// LastIssueUpdateAt is the timestamp of the project's most recent issue
	// update (RFC 3339). Optional.
	LastIssueUpdateAt *string `json:"last_issue_update_at,omitempty"`

	// IssueCount is the provider-reported project issue count. Optional.
	IssueCount *int64 `json:"issue_count,omitempty"`
}

// WorkItemIssueTypeMetadata is the schema-version-1 typed payload for the
// "work_item.issue_type_metadata" fact kind: one provider issue-type
// definition.
//
// The required set matches the collector emitter
// (jira.NewWorkItemIssueTypeMetadataEnvelope), which rejects a blank issue-type
// id and always stamps the provider token.
type WorkItemIssueTypeMetadata struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// IssueTypeID is the provider issue-type id. Required — the emitter
	// rejects a blank issue-type id.
	IssueTypeID string `json:"issue_type_id"`

	// ProjectID is the provider project id the issue type is scoped to.
	// Optional: blank for a global (non-project-scoped) issue type.
	ProjectID *string `json:"project_id,omitempty"`

	// ScopeType is the provider scope-type token (for example "PROJECT").
	// Optional.
	ScopeType *string `json:"scope_type,omitempty"`

	// EntityID is always redacted to the empty string by the collector.
	// Optional; see EntityIDPresent and EntityIDFingerprint.
	EntityID *string `json:"entity_id,omitempty"`

	// EntityIDPresent reports whether the source issue type carried a
	// non-blank entity id before redaction. Optional.
	EntityIDPresent *bool `json:"entity_id_present,omitempty"`

	// EntityIDFingerprint is a normalized sha256 fingerprint of the
	// (redacted) entity id. Optional.
	EntityIDFingerprint *string `json:"entity_id_fingerprint,omitempty"`

	// IssueTypeName is always redacted to the empty string by the collector.
	// Optional; see IssueTypeNamePresent and IssueTypeNameFingerprint.
	IssueTypeName *string `json:"issue_type_name,omitempty"`

	// IssueTypeNamePresent reports whether the source issue type carried a
	// non-blank name before redaction. Optional.
	IssueTypeNamePresent *bool `json:"issue_type_name_present,omitempty"`

	// IssueTypeNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) issue-type name. Optional.
	IssueTypeNameFingerprint *string `json:"issue_type_name_fingerprint,omitempty"`

	// DescriptionPresent reports whether the source issue type carried a
	// non-blank description. Optional.
	DescriptionPresent *bool `json:"description_present,omitempty"`

	// DescriptionFingerprint is a normalized sha256 fingerprint of the
	// (redacted) description. Optional.
	DescriptionFingerprint *string `json:"description_fingerprint,omitempty"`

	// HierarchyLevel is the provider issue-type hierarchy level. Optional.
	HierarchyLevel *int64 `json:"hierarchy_level,omitempty"`

	// Subtask reports whether the issue type is a subtask type. Optional.
	Subtask *bool `json:"subtask,omitempty"`

	// SelfURL is always redacted to the empty string by the collector.
	// Optional; see SelfURLFingerprint.
	SelfURL *string `json:"self_url,omitempty"`

	// SelfURLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// self URL. Optional.
	SelfURLFingerprint *string `json:"self_url_fingerprint,omitempty"`
}

// WorkItemStatusMetadata is the schema-version-1 typed payload for the
// "work_item.status_metadata" fact kind: one provider workflow status
// definition.
//
// The required set matches the collector emitter
// (jira.NewWorkItemStatusMetadataEnvelope), which rejects a blank status id
// and always stamps the provider token.
type WorkItemStatusMetadata struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// StatusID is the provider status id. Required — the emitter rejects a
	// blank status id.
	StatusID string `json:"status_id"`

	// ProjectID is the provider project id the status is scoped to. Optional:
	// blank for a global status.
	ProjectID *string `json:"project_id,omitempty"`

	// ScopeType is the provider scope-type token. Optional.
	ScopeType *string `json:"scope_type,omitempty"`

	// StatusName is always redacted to the empty string by the collector.
	// Optional; see StatusNamePresent and StatusNameFingerprint.
	StatusName *string `json:"status_name,omitempty"`

	// StatusNamePresent reports whether the source status carried a
	// non-blank name before redaction. Optional.
	StatusNamePresent *bool `json:"status_name_present,omitempty"`

	// StatusNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) status name. Optional.
	StatusNameFingerprint *string `json:"status_name_fingerprint,omitempty"`

	// DescriptionPresent reports whether the source status carried a
	// non-blank description. Optional.
	DescriptionPresent *bool `json:"description_present,omitempty"`

	// DescriptionFingerprint is a normalized sha256 fingerprint of the
	// (redacted) description. Optional.
	DescriptionFingerprint *string `json:"description_fingerprint,omitempty"`

	// StatusCategory is the provider status category (for example "Done").
	// Optional.
	StatusCategory *string `json:"status_category,omitempty"`

	// StatusCategoryKey is the provider status category key (for example
	// "done"). Optional.
	StatusCategoryKey *string `json:"status_category_key,omitempty"`

	// SelfURL is always redacted to the empty string by the collector.
	// Optional; see SelfURLFingerprint.
	SelfURL *string `json:"self_url,omitempty"`

	// SelfURLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// self URL. Optional.
	SelfURLFingerprint *string `json:"self_url_fingerprint,omitempty"`
}

// WorkItemWorkflowStatus is one sanitized status reference inside a workflow
// definition, as the emitter shapes it through workflowStatusPayloads. Every
// field is optional because the type is a list element, not the top-level
// fact payload; a genuinely empty status reference is still a valid list
// element.
type WorkItemWorkflowStatus struct {
	// StatusReference is the provider workflow status reference token.
	// Optional.
	StatusReference *string `json:"status_reference,omitempty"`

	// StatusID is the provider status id the reference resolves to. Optional.
	StatusID *string `json:"status_id,omitempty"`

	// Deprecated reports whether the provider marks this status reference
	// deprecated. Optional.
	Deprecated *bool `json:"deprecated,omitempty"`
}

// WorkItemWorkflowTransition is one sanitized transition shape inside a
// workflow definition, as the emitter shapes it through
// workflowTransitionPayloads. Every field is optional for the same reason as
// WorkItemWorkflowStatus.
type WorkItemWorkflowTransition struct {
	// TransitionID is the provider transition id. Optional.
	TransitionID *string `json:"transition_id,omitempty"`

	// TransitionName is always redacted to the empty string by the
	// collector. Optional; see TransitionNamePresent and
	// TransitionNameFingerprint.
	TransitionName *string `json:"transition_name,omitempty"`

	// TransitionNamePresent reports whether the source transition carried a
	// non-blank name before redaction. Optional.
	TransitionNamePresent *bool `json:"transition_name_present,omitempty"`

	// TransitionNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) transition name. Optional.
	TransitionNameFingerprint *string `json:"transition_name_fingerprint,omitempty"`

	// TransitionType is the provider transition type token (for example
	// "DIRECTED"). Optional.
	TransitionType *string `json:"transition_type,omitempty"`

	// FromStatusReferences are the workflow status references this
	// transition can start from, sorted. Optional.
	FromStatusReferences []string `json:"from_status_references,omitempty"`

	// ToStatusReference is the workflow status reference this transition
	// ends at. Optional.
	ToStatusReference *string `json:"to_status_reference,omitempty"`

	// HasValidators reports whether the provider transition carries
	// validators. Optional.
	HasValidators *bool `json:"has_validators,omitempty"`

	// HasTriggers reports whether the provider transition carries triggers.
	// Optional.
	HasTriggers *bool `json:"has_triggers,omitempty"`

	// HasActions reports whether the provider transition carries post
	// functions/actions. Optional.
	HasActions *bool `json:"has_actions,omitempty"`
}

// WorkItemWorkflowMetadata is the schema-version-1 typed payload for the
// "work_item.workflow_metadata" fact kind: one provider workflow definition.
//
// The required set matches the collector emitter
// (jira.NewWorkItemWorkflowMetadataEnvelope), which rejects a blank workflow id
// and always stamps the provider token. Statuses and Transitions are nested
// typed lists (Wave-3 nested support) mirroring workflowStatusPayloads /
// workflowTransitionPayloads.
type WorkItemWorkflowMetadata struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// WorkflowID is the provider workflow id. Required — the emitter rejects
	// a blank workflow id.
	WorkflowID string `json:"workflow_id"`

	// WorkflowName is always redacted to the empty string by the collector.
	// Optional; see WorkflowNamePresent and WorkflowNameFingerprint.
	WorkflowName *string `json:"workflow_name,omitempty"`

	// WorkflowNamePresent reports whether the source workflow carried a
	// non-blank name before redaction. Optional.
	WorkflowNamePresent *bool `json:"workflow_name_present,omitempty"`

	// WorkflowNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) workflow name. Optional.
	WorkflowNameFingerprint *string `json:"workflow_name_fingerprint,omitempty"`

	// DescriptionPresent reports whether the source workflow carried a
	// non-blank description. Optional.
	DescriptionPresent *bool `json:"description_present,omitempty"`

	// DescriptionFingerprint is a normalized sha256 fingerprint of the
	// (redacted) description. Optional.
	DescriptionFingerprint *string `json:"description_fingerprint,omitempty"`

	// ScopeType is the provider scope-type token. Optional.
	ScopeType *string `json:"scope_type,omitempty"`

	// ProjectID is the provider project id the workflow is scoped to.
	// Optional.
	ProjectID *string `json:"project_id,omitempty"`

	// VersionID is always redacted to the empty string by the collector.
	// Optional; see VersionIDPresent and VersionIDFingerprint.
	VersionID *string `json:"version_id,omitempty"`

	// VersionIDPresent reports whether the source workflow version carried a
	// non-blank id before redaction. Optional.
	VersionIDPresent *bool `json:"version_id_present,omitempty"`

	// VersionIDFingerprint is a normalized sha256 fingerprint of the
	// (redacted) version id. Optional.
	VersionIDFingerprint *string `json:"version_id_fingerprint,omitempty"`

	// VersionNumber is the provider workflow version number. Optional.
	VersionNumber *int64 `json:"version_number,omitempty"`

	// Statuses are the sanitized status references this workflow carries.
	// Optional: nil when the workflow has no statuses.
	Statuses []WorkItemWorkflowStatus `json:"statuses,omitempty"`

	// Transitions are the sanitized transition shapes this workflow carries.
	// Optional: nil when the workflow has no transitions.
	Transitions []WorkItemWorkflowTransition `json:"transitions,omitempty"`
}

// WorkItemFieldMetadata is the schema-version-1 typed payload for the
// "work_item.field_metadata" fact kind: one provider custom/system field
// definition.
//
// The collector emitter (jira.NewWorkItemFieldMetadataEnvelope) rejects a
// blank field id at the Go level, but the payload's own "field_id" key is
// ALWAYS emitted as the redacted empty string (only "field_id_fingerprint"
// carries the derived identity) — so, unlike every other kind in this family,
// FieldID cannot be the required identity anchor without dead-lettering every
// valid field-metadata fact. Only Provider is required here.
type WorkItemFieldMetadata struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// FieldID is always redacted to the empty string by the collector.
	// Optional: see FieldIDPresent and FieldIDFingerprint for the identity
	// signal; this field can never be a required anchor because the emitter
	// always stamps it "".
	FieldID *string `json:"field_id,omitempty"`

	// FieldIDPresent is always true when the emitter observed a field id (the
	// emitter only builds this envelope when the field id is non-blank).
	// Optional.
	FieldIDPresent *bool `json:"field_id_present,omitempty"`

	// FieldIDFingerprint is a normalized sha256 fingerprint of the (redacted)
	// field id — the durable identity anchor for this kind at the read-model
	// layer, though not enforced as schema-required (see the type doc).
	// Optional.
	FieldIDFingerprint *string `json:"field_id_fingerprint,omitempty"`

	// FieldName is always redacted to the empty string by the collector.
	// Optional; see FieldNamePresent and FieldNameFingerprint.
	FieldName *string `json:"field_name,omitempty"`

	// FieldNamePresent reports whether the source field carried a non-blank
	// name before redaction. Optional.
	FieldNamePresent *bool `json:"field_name_present,omitempty"`

	// FieldNameFingerprint is a normalized sha256 fingerprint of the
	// (redacted) field name. Optional.
	FieldNameFingerprint *string `json:"field_name_fingerprint,omitempty"`

	// DescriptionPresent reports whether the source field carried a
	// non-blank description. Optional.
	DescriptionPresent *bool `json:"description_present,omitempty"`

	// DescriptionFingerprint is a normalized sha256 fingerprint of the
	// (redacted) description. Optional.
	DescriptionFingerprint *string `json:"description_fingerprint,omitempty"`

	// SchemaType is the provider field schema type token. Optional.
	SchemaType *string `json:"schema_type,omitempty"`

	// SchemaItems is the provider field schema items token (for an array
	// field). Optional.
	SchemaItems *string `json:"schema_items,omitempty"`

	// SchemaSystem is the provider field schema system token. Optional.
	SchemaSystem *string `json:"schema_system,omitempty"`

	// SchemaCustom is the provider field schema custom-type token. Optional.
	SchemaCustom *string `json:"schema_custom,omitempty"`

	// CustomIDPresent reports whether the source field carried a non-blank
	// custom-field numeric id. Optional.
	CustomIDPresent *bool `json:"custom_id_present,omitempty"`

	// SelfURL is always redacted to the empty string by the collector.
	// Optional; see SelfURLFingerprint.
	SelfURL *string `json:"self_url,omitempty"`

	// SelfURLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// self URL. Optional.
	SelfURLFingerprint *string `json:"self_url_fingerprint,omitempty"`
}

// WorkItemMetadataWarning is the schema-version-1 typed payload for the
// "work_item.metadata_warning" fact kind: a metadata collection warning that
// must remain visible to readers instead of being confused with empty
// metadata.
//
// The required set matches the collector emitter
// (jira.NewWorkItemMetadataWarningEnvelope), which rejects a blank metadata
// type or reason and always stamps the provider token.
type WorkItemMetadataWarning struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// CollectorInstanceID is the collector boundary token. Optional.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// MetadataType names the metadata class the warning applies to (for
	// example "workflow"). Required — the emitter rejects a blank metadata
	// type.
	MetadataType string `json:"metadata_type"`

	// Reason is the provider-facing warning reason token. Required — the
	// emitter rejects a blank reason.
	Reason string `json:"reason"`

	// FailureClass is the bounded retry/failure classification token.
	// Optional.
	FailureClass *string `json:"failure_class,omitempty"`

	// ProviderID is always redacted to the empty string by the collector.
	// Optional; see ProviderIDPresent and ProviderIDFingerprint.
	ProviderID *string `json:"provider_id,omitempty"`

	// ProviderIDPresent reports whether the source warning carried a
	// non-blank provider id before redaction. Optional.
	ProviderIDPresent *bool `json:"provider_id_present,omitempty"`

	// ProviderIDFingerprint is a normalized sha256 fingerprint of the
	// (redacted) provider id. Optional.
	ProviderIDFingerprint *string `json:"provider_id_fingerprint,omitempty"`
}
