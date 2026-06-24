// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "slices"

const (
	// WorkItemRecordFactKind identifies one provider work item, such as a Jira
	// issue, as source-reported work tracking evidence.
	WorkItemRecordFactKind = "work_item.record"
	// WorkItemTransitionFactKind identifies one provider work item changelog
	// transition or field change as source-reported lifecycle evidence.
	WorkItemTransitionFactKind = "work_item.transition"
	// WorkItemExternalLinkFactKind identifies one provider remote link attached
	// to a work item as source-reported cross-system evidence.
	WorkItemExternalLinkFactKind = "work_item.external_link"
	// WorkItemProjectMetadataFactKind identifies one provider project metadata
	// definition as source-reported work tracking context.
	WorkItemProjectMetadataFactKind = "work_item.project_metadata"
	// WorkItemIssueTypeMetadataFactKind identifies one provider issue-type
	// metadata definition as source-reported work tracking context.
	WorkItemIssueTypeMetadataFactKind = "work_item.issue_type_metadata"
	// WorkItemStatusMetadataFactKind identifies one provider status metadata
	// definition as source-reported work tracking context.
	WorkItemStatusMetadataFactKind = "work_item.status_metadata"
	// WorkItemWorkflowMetadataFactKind identifies one provider workflow metadata
	// definition as source-reported transition context.
	WorkItemWorkflowMetadataFactKind = "work_item.workflow_metadata"
	// WorkItemFieldMetadataFactKind identifies one provider field metadata
	// definition as source-reported custom-field context.
	WorkItemFieldMetadataFactKind = "work_item.field_metadata"
	// WorkItemMetadataWarningFactKind identifies metadata collection warnings
	// that distinguish unsupported, stale, or permission-hidden definitions
	// from empty metadata.
	WorkItemMetadataWarningFactKind = "work_item.metadata_warning"

	// WorkItemSchemaVersionV1 is the first work-item fact schema.
	WorkItemSchemaVersionV1 = "1.0.0"
)

var workItemFactKinds = []string{
	WorkItemRecordFactKind,
	WorkItemTransitionFactKind,
	WorkItemExternalLinkFactKind,
	WorkItemProjectMetadataFactKind,
	WorkItemIssueTypeMetadataFactKind,
	WorkItemStatusMetadataFactKind,
	WorkItemWorkflowMetadataFactKind,
	WorkItemFieldMetadataFactKind,
	WorkItemMetadataWarningFactKind,
}

var workItemSchemaVersions = map[string]string{
	WorkItemRecordFactKind:            WorkItemSchemaVersionV1,
	WorkItemTransitionFactKind:        WorkItemSchemaVersionV1,
	WorkItemExternalLinkFactKind:      WorkItemSchemaVersionV1,
	WorkItemProjectMetadataFactKind:   WorkItemSchemaVersionV1,
	WorkItemIssueTypeMetadataFactKind: WorkItemSchemaVersionV1,
	WorkItemStatusMetadataFactKind:    WorkItemSchemaVersionV1,
	WorkItemWorkflowMetadataFactKind:  WorkItemSchemaVersionV1,
	WorkItemFieldMetadataFactKind:     WorkItemSchemaVersionV1,
	WorkItemMetadataWarningFactKind:   WorkItemSchemaVersionV1,
}

// WorkItemFactKinds returns the accepted work-item fact kinds in
// source-contract order.
func WorkItemFactKinds() []string {
	return slices.Clone(workItemFactKinds)
}

// WorkItemSchemaVersion returns the schema version for a work-item fact kind.
func WorkItemSchemaVersion(factKind string) (string, bool) {
	version, ok := workItemSchemaVersions[factKind]
	return version, ok
}
