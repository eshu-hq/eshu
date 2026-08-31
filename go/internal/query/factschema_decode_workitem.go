// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querydecode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// This file holds the query-side decode wrappers for the work_item fact
// family — the ONLY decode site for work_item.* payloads in this codebase (no
// reducer or projector domain consumes them; see
// sdk/go/factschema/workitem/v1/README.md). Each wraps the contracts-module
// Decode* seam and, on a classified *factschema.DecodeError (a missing/null
// required identity field), returns a *queryDecodeError so the caller can
// route it to an input_invalid-class read-model outcome (a dropped row, not an
// empty-identity row) instead of silently defaulting every field to "".
//
// This mirrors the projector's factschema_quarantine.go pattern
// (partitionProjectorDecodeFailures / newProjectorDecodeError /
// factschemaEnvelope) at the scope this package actually needs: the query
// layer never quarantines a durable fact record (it is a read path, not a
// write path), it classifies one decoded row as unusable for the response it
// is building.

// queryDecodeError is the query layer's classified decode failure. It aliases
// querydecode.Error, which owns the type and both of its methods.
//
// The alias carries Error() and Unwrap() because they are exported; #6060's
// other seam, RepositoryAccessFilter, needed a 177-file rename precisely
// because ITS methods were unexported and an alias cannot reach those across a
// package boundary. Nothing here changes for the 73 existing references.
type queryDecodeError = querydecode.Error

// newQueryDecodeError wraps a decode error returned by a factschema Decode*
// function into the query layer's classified decode failure.
func newQueryDecodeError(factKind, factID string, err error) *queryDecodeError {
	return querydecode.New(factKind, factID, err)
}

// workItemSchemaEnvelope adapts one scanned work-item fact row into the
// contracts-module factschema.Envelope the Decode* seam accepts. An empty
// schemaVersion is normalized to the current major-1 schema version — every
// Jira work-item emitter stamps a concrete "1.0.0" version
// (facts.WorkItemSchemaVersionV1), so a version-less row does not occur on the
// production path; a present but unsupported major still dead-letters through
// the Decode* seam's default branch.
func workItemSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = queryDefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// queryDefaultSchemaMajorVersion is the schema version this package assumes
// when a row carries none. It is a major-1 version because every migrated
// work_item fact kind is at schema major 1 today; the Decode seam dispatches
// on the major component only.
const queryDefaultSchemaMajorVersion = "1.0.0"

// workItemDecodeInput carries one scanned work-item fact row into a decode
// wrapper. Bundling the row's identity, schema version, and payload into a
// single parameter lets each decode wrapper keep the one-argument shape the
// payload-usage manifest gate's seam parser recognizes (a decode<Kind> func
// taking one value and returning (workitemv1.<Struct>, error)); a three-arg
// wrapper would be invisible to the gate, leaving the query decode sites
// silently ungated. FactID is retained only for operator-facing error
// attribution, never for decode input.
type workItemDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// decodeWorkItemRecord decodes one work_item.record fact row into the typed
// struct through the contracts seam. A missing required field
// (provider_work_item_id, work_item_key) yields a self-classifying
// *queryDecodeError.
func decodeWorkItemRecord(in workItemDecodeInput) (workitemv1.WorkItemRecord, error) {
	record, err := factschema.DecodeWorkItemRecord(workItemSchemaEnvelope(factschema.FactKindWorkItemRecord, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemRecord{}, newQueryDecodeError(factschema.FactKindWorkItemRecord, in.FactID, err)
	}
	return record, nil
}

// decodeWorkItemTransition decodes one work_item.transition fact row into the
// typed struct. A missing required field (provider_changelog_id) yields a
// self-classifying *queryDecodeError.
func decodeWorkItemTransition(in workItemDecodeInput) (workitemv1.WorkItemTransition, error) {
	transition, err := factschema.DecodeWorkItemTransition(workItemSchemaEnvelope(factschema.FactKindWorkItemTransition, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemTransition{}, newQueryDecodeError(factschema.FactKindWorkItemTransition, in.FactID, err)
	}
	return transition, nil
}

// decodeWorkItemExternalLink decodes one work_item.external_link fact row
// into the typed struct. Only "provider" is required for this kind (see
// workitem/v1/README.md), so this rarely dead-letters.
func decodeWorkItemExternalLink(in workItemDecodeInput) (workitemv1.WorkItemExternalLink, error) {
	link, err := factschema.DecodeWorkItemExternalLink(workItemSchemaEnvelope(factschema.FactKindWorkItemExternalLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemExternalLink{}, newQueryDecodeError(factschema.FactKindWorkItemExternalLink, in.FactID, err)
	}
	return link, nil
}

// decodeWorkItemProjectMetadata decodes one work_item.project_metadata fact
// row into the typed struct. Only "provider" is required for this kind.
func decodeWorkItemProjectMetadata(in workItemDecodeInput) (workitemv1.WorkItemProjectMetadata, error) {
	metadata, err := factschema.DecodeWorkItemProjectMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemProjectMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemProjectMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemProjectMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemIssueTypeMetadata decodes one work_item.issue_type_metadata
// fact row into the typed struct. A missing required field (provider,
// issue_type_id) yields a self-classifying *queryDecodeError.
func decodeWorkItemIssueTypeMetadata(in workItemDecodeInput) (workitemv1.WorkItemIssueTypeMetadata, error) {
	metadata, err := factschema.DecodeWorkItemIssueTypeMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemIssueTypeMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemIssueTypeMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemIssueTypeMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemStatusMetadata decodes one work_item.status_metadata fact row
// into the typed struct. A missing required field (status_id) yields a
// self-classifying *queryDecodeError.
func decodeWorkItemStatusMetadata(in workItemDecodeInput) (workitemv1.WorkItemStatusMetadata, error) {
	metadata, err := factschema.DecodeWorkItemStatusMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemStatusMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemStatusMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemStatusMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemWorkflowMetadata decodes one work_item.workflow_metadata fact
// row into the typed struct. A missing required field (workflow_id) yields a
// self-classifying *queryDecodeError.
func decodeWorkItemWorkflowMetadata(in workItemDecodeInput) (workitemv1.WorkItemWorkflowMetadata, error) {
	metadata, err := factschema.DecodeWorkItemWorkflowMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemWorkflowMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemWorkflowMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemWorkflowMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemFieldMetadata decodes one work_item.field_metadata fact row
// into the typed struct. Only "provider" is required for this kind — the
// payload's own field_id is always redacted to "" by the collector (see
// workitem/v1/README.md), so this rarely dead-letters.
func decodeWorkItemFieldMetadata(in workItemDecodeInput) (workitemv1.WorkItemFieldMetadata, error) {
	metadata, err := factschema.DecodeWorkItemFieldMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemFieldMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemFieldMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemFieldMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemMetadataWarning decodes one work_item.metadata_warning fact
// row into the typed struct. A missing required field (metadata_type, reason)
// yields a self-classifying *queryDecodeError.
func decodeWorkItemMetadataWarning(in workItemDecodeInput) (workitemv1.WorkItemMetadataWarning, error) {
	warning, err := factschema.DecodeWorkItemMetadataWarning(workItemSchemaEnvelope(factschema.FactKindWorkItemMetadataWarning, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemMetadataWarning{}, newQueryDecodeError(factschema.FactKindWorkItemMetadataWarning, in.FactID, err)
	}
	return warning, nil
}

// workItemDerefString returns the value a *string points at, or "" when it is
// nil, matching the pre-typing StringVal("") behavior for a field this
// migration converts from a raw payload lookup to a typed pointer.
func workItemDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// workItemDerefBool returns the value a *bool points at, or false when it is
// nil, matching the pre-typing BoolVal(false) behavior.
func workItemDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
