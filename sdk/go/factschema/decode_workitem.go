// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// DecodeWorkItemRecord decodes env.Payload into the latest
// workitemv1.WorkItemRecord struct for the "work_item.record" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2. Callers
// (the query read-model layer) receive either the decoded struct or a
// classified *DecodeError; they must never substitute a zero-value struct on
// error. A payload missing the required provider, provider_work_item_id, or
// work_item_key key dead-letters as input_invalid rather than producing an
// empty-string work-item identity.
func DecodeWorkItemRecord(env Envelope) (workitemv1.WorkItemRecord, error) {
	return decodeLatestMajor[workitemv1.WorkItemRecord](FactKindWorkItemRecord, env)
}

// EncodeWorkItemRecord marshals a workitemv1.WorkItemRecord into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeWorkItemRecord for schema-version-1 payloads, used by this module's
// round-trip tests.
func EncodeWorkItemRecord(record workitemv1.WorkItemRecord) (map[string]any, error) {
	return encodeToPayload(record)
}

// DecodeWorkItemTransition decodes env.Payload into the latest
// workitemv1.WorkItemTransition struct for the "work_item.transition" fact
// kind. See DecodeWorkItemRecord for the dispatch and error contract.
func DecodeWorkItemTransition(env Envelope) (workitemv1.WorkItemTransition, error) {
	return decodeLatestMajor[workitemv1.WorkItemTransition](FactKindWorkItemTransition, env)
}

// EncodeWorkItemTransition marshals a workitemv1.WorkItemTransition into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeWorkItemTransition for schema-version-1 payloads.
func EncodeWorkItemTransition(transition workitemv1.WorkItemTransition) (map[string]any, error) {
	return encodeToPayload(transition)
}

// DecodeWorkItemExternalLink decodes env.Payload into the latest
// workitemv1.WorkItemExternalLink struct for the "work_item.external_link"
// fact kind. See DecodeWorkItemRecord for the dispatch and error contract.
func DecodeWorkItemExternalLink(env Envelope) (workitemv1.WorkItemExternalLink, error) {
	return decodeLatestMajor[workitemv1.WorkItemExternalLink](FactKindWorkItemExternalLink, env)
}

// EncodeWorkItemExternalLink marshals a workitemv1.WorkItemExternalLink into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeWorkItemExternalLink for schema-version-1 payloads.
func EncodeWorkItemExternalLink(link workitemv1.WorkItemExternalLink) (map[string]any, error) {
	return encodeToPayload(link)
}

// DecodeWorkItemProjectMetadata decodes env.Payload into the latest
// workitemv1.WorkItemProjectMetadata struct for the
// "work_item.project_metadata" fact kind. See DecodeWorkItemRecord for the
// dispatch and error contract.
func DecodeWorkItemProjectMetadata(env Envelope) (workitemv1.WorkItemProjectMetadata, error) {
	return decodeLatestMajor[workitemv1.WorkItemProjectMetadata](FactKindWorkItemProjectMetadata, env)
}

// EncodeWorkItemProjectMetadata marshals a workitemv1.WorkItemProjectMetadata
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeWorkItemProjectMetadata for schema-version-1 payloads.
func EncodeWorkItemProjectMetadata(metadata workitemv1.WorkItemProjectMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeWorkItemIssueTypeMetadata decodes env.Payload into the latest
// workitemv1.WorkItemIssueTypeMetadata struct for the
// "work_item.issue_type_metadata" fact kind. See DecodeWorkItemRecord for the
// dispatch and error contract.
func DecodeWorkItemIssueTypeMetadata(env Envelope) (workitemv1.WorkItemIssueTypeMetadata, error) {
	return decodeLatestMajor[workitemv1.WorkItemIssueTypeMetadata](FactKindWorkItemIssueTypeMetadata, env)
}

// EncodeWorkItemIssueTypeMetadata marshals a
// workitemv1.WorkItemIssueTypeMetadata into the map[string]any payload shape
// an Envelope carries. It is the inverse of DecodeWorkItemIssueTypeMetadata
// for schema-version-1 payloads.
func EncodeWorkItemIssueTypeMetadata(metadata workitemv1.WorkItemIssueTypeMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeWorkItemStatusMetadata decodes env.Payload into the latest
// workitemv1.WorkItemStatusMetadata struct for the
// "work_item.status_metadata" fact kind. See DecodeWorkItemRecord for the
// dispatch and error contract.
func DecodeWorkItemStatusMetadata(env Envelope) (workitemv1.WorkItemStatusMetadata, error) {
	return decodeLatestMajor[workitemv1.WorkItemStatusMetadata](FactKindWorkItemStatusMetadata, env)
}

// EncodeWorkItemStatusMetadata marshals a workitemv1.WorkItemStatusMetadata
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeWorkItemStatusMetadata for schema-version-1 payloads.
func EncodeWorkItemStatusMetadata(metadata workitemv1.WorkItemStatusMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeWorkItemWorkflowMetadata decodes env.Payload into the latest
// workitemv1.WorkItemWorkflowMetadata struct for the
// "work_item.workflow_metadata" fact kind. See DecodeWorkItemRecord for the
// dispatch and error contract.
func DecodeWorkItemWorkflowMetadata(env Envelope) (workitemv1.WorkItemWorkflowMetadata, error) {
	return decodeLatestMajor[workitemv1.WorkItemWorkflowMetadata](FactKindWorkItemWorkflowMetadata, env)
}

// EncodeWorkItemWorkflowMetadata marshals a
// workitemv1.WorkItemWorkflowMetadata into the map[string]any payload shape
// an Envelope carries. It is the inverse of DecodeWorkItemWorkflowMetadata for
// schema-version-1 payloads.
func EncodeWorkItemWorkflowMetadata(metadata workitemv1.WorkItemWorkflowMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeWorkItemFieldMetadata decodes env.Payload into the latest
// workitemv1.WorkItemFieldMetadata struct for the "work_item.field_metadata"
// fact kind. See DecodeWorkItemRecord for the dispatch and error contract.
func DecodeWorkItemFieldMetadata(env Envelope) (workitemv1.WorkItemFieldMetadata, error) {
	return decodeLatestMajor[workitemv1.WorkItemFieldMetadata](FactKindWorkItemFieldMetadata, env)
}

// EncodeWorkItemFieldMetadata marshals a workitemv1.WorkItemFieldMetadata
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeWorkItemFieldMetadata for schema-version-1 payloads.
func EncodeWorkItemFieldMetadata(metadata workitemv1.WorkItemFieldMetadata) (map[string]any, error) {
	return encodeToPayload(metadata)
}

// DecodeWorkItemMetadataWarning decodes env.Payload into the latest
// workitemv1.WorkItemMetadataWarning struct for the
// "work_item.metadata_warning" fact kind. See DecodeWorkItemRecord for the
// dispatch and error contract.
func DecodeWorkItemMetadataWarning(env Envelope) (workitemv1.WorkItemMetadataWarning, error) {
	return decodeLatestMajor[workitemv1.WorkItemMetadataWarning](FactKindWorkItemMetadataWarning, env)
}

// EncodeWorkItemMetadataWarning marshals a workitemv1.WorkItemMetadataWarning
// into the map[string]any payload shape an Envelope carries. It is the
// inverse of DecodeWorkItemMetadataWarning for schema-version-1 payloads.
func EncodeWorkItemMetadataWarning(warning workitemv1.WorkItemMetadataWarning) (map[string]any, error) {
	return encodeToPayload(warning)
}
