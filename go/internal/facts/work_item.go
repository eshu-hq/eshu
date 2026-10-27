// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

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

// WorkItemFamilyDeclaration is the typed fact-family contract declaration for the
// work_item family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var WorkItemFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "work_item",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "incident_repository_correlation",
	ProjectionHook:         "work_item_evidence_read_model",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/work-items/evidence",
	TruthProfile:           "provider_gated",
	ProviderKeyIndependent: false,
	Kinds: []contract.Kind{
		{Kind: "work_item.external_link", PayloadSchema: "sdk/go/factschema/schema/work_item.external_link.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.field_metadata", PayloadSchema: "sdk/go/factschema/schema/work_item.field_metadata.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.issue_type_metadata", PayloadSchema: "sdk/go/factschema/schema/work_item.issue_type_metadata.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.metadata_warning", PayloadSchema: "sdk/go/factschema/schema/work_item.metadata_warning.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.project_metadata", PayloadSchema: "sdk/go/factschema/schema/work_item.project_metadata.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.record", PayloadSchema: "sdk/go/factschema/schema/work_item.record.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.status_metadata", PayloadSchema: "sdk/go/factschema/schema/work_item.status_metadata.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.transition", PayloadSchema: "sdk/go/factschema/schema/work_item.transition.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
		{Kind: "work_item.workflow_metadata", PayloadSchema: "sdk/go/factschema/schema/work_item.workflow_metadata.v1.schema.json", Disposition: "read_only", NoReducerReason: "Consumed by read surfaces only; no reducer domain decodes it.", NoReducerEvidence: "read consumption in go/internal/query/decode/workitem, go/internal/query/workitem"},
	},
	Producers: []contract.Producer{
		{ID: "collector-jira", Repository: "eshu-hq/eshu-collector-jira", Path: ".", PlannedRepository: "", AuthorityMode: "core_issued_grant", Kinds: []string{"work_item.external_link", "work_item.field_metadata", "work_item.issue_type_metadata", "work_item.metadata_warning", "work_item.project_metadata", "work_item.record", "work_item.status_metadata", "work_item.transition", "work_item.workflow_metadata"}},
	},
})
