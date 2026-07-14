// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemagen

import (
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// WorkItemRecordSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.record" payload.
const WorkItemRecordSchemaID = schemaBaseID + "workitem/v1/record.schema.json"

// WorkItemRecordSchema returns the JSON Schema bytes for
// workitemv1.WorkItemRecord.
func WorkItemRecordSchema() ([]byte, error) {
	return reflectSchema(WorkItemRecordSchemaID, "Eshu work_item.record Payload (schema version 1)", &workitemv1.WorkItemRecord{})
}

// WorkItemTransitionSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.transition" payload.
const WorkItemTransitionSchemaID = schemaBaseID + "workitem/v1/transition.schema.json"

// WorkItemTransitionSchema returns the JSON Schema bytes for
// workitemv1.WorkItemTransition.
func WorkItemTransitionSchema() ([]byte, error) {
	return reflectSchema(WorkItemTransitionSchemaID, "Eshu work_item.transition Payload (schema version 1)", &workitemv1.WorkItemTransition{})
}

// WorkItemExternalLinkSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.external_link" payload.
const WorkItemExternalLinkSchemaID = schemaBaseID + "workitem/v1/external_link.schema.json"

// WorkItemExternalLinkSchema returns the JSON Schema bytes for
// workitemv1.WorkItemExternalLink.
func WorkItemExternalLinkSchema() ([]byte, error) {
	return reflectSchema(WorkItemExternalLinkSchemaID, "Eshu work_item.external_link Payload (schema version 1)", &workitemv1.WorkItemExternalLink{})
}

// WorkItemProjectMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.project_metadata" payload.
const WorkItemProjectMetadataSchemaID = schemaBaseID + "workitem/v1/project_metadata.schema.json"

// WorkItemProjectMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemProjectMetadata.
func WorkItemProjectMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemProjectMetadataSchemaID, "Eshu work_item.project_metadata Payload (schema version 1)", &workitemv1.WorkItemProjectMetadata{})
}

// WorkItemIssueTypeMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.issue_type_metadata" payload.
const WorkItemIssueTypeMetadataSchemaID = schemaBaseID + "workitem/v1/issue_type_metadata.schema.json"

// WorkItemIssueTypeMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemIssueTypeMetadata.
func WorkItemIssueTypeMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemIssueTypeMetadataSchemaID, "Eshu work_item.issue_type_metadata Payload (schema version 1)", &workitemv1.WorkItemIssueTypeMetadata{})
}

// WorkItemStatusMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.status_metadata" payload.
const WorkItemStatusMetadataSchemaID = schemaBaseID + "workitem/v1/status_metadata.schema.json"

// WorkItemStatusMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemStatusMetadata.
func WorkItemStatusMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemStatusMetadataSchemaID, "Eshu work_item.status_metadata Payload (schema version 1)", &workitemv1.WorkItemStatusMetadata{})
}

// WorkItemWorkflowMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.workflow_metadata" payload.
const WorkItemWorkflowMetadataSchemaID = schemaBaseID + "workitem/v1/workflow_metadata.schema.json"

// WorkItemWorkflowMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemWorkflowMetadata.
func WorkItemWorkflowMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemWorkflowMetadataSchemaID, "Eshu work_item.workflow_metadata Payload (schema version 1)", &workitemv1.WorkItemWorkflowMetadata{})
}

// WorkItemFieldMetadataSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.field_metadata" payload.
const WorkItemFieldMetadataSchemaID = schemaBaseID + "workitem/v1/field_metadata.schema.json"

// WorkItemFieldMetadataSchema returns the JSON Schema bytes for
// workitemv1.WorkItemFieldMetadata.
func WorkItemFieldMetadataSchema() ([]byte, error) {
	return reflectSchema(WorkItemFieldMetadataSchemaID, "Eshu work_item.field_metadata Payload (schema version 1)", &workitemv1.WorkItemFieldMetadata{})
}

// WorkItemMetadataWarningSchemaID is the checked-in JSON Schema $id for the
// schema-version-1 "work_item.metadata_warning" payload.
const WorkItemMetadataWarningSchemaID = schemaBaseID + "workitem/v1/metadata_warning.schema.json"

// WorkItemMetadataWarningSchema returns the JSON Schema bytes for
// workitemv1.WorkItemMetadataWarning.
func WorkItemMetadataWarningSchema() ([]byte, error) {
	return reflectSchema(WorkItemMetadataWarningSchemaID, "Eshu work_item.metadata_warning Payload (schema version 1)", &workitemv1.WorkItemMetadataWarning{})
}
