// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workitem

import (
	"github.com/eshu-hq/eshu/go/internal/query/decode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// This file holds the decode wrappers for the work_item fact family — the
// ONLY decode site for work_item.* payloads in this codebase (no reducer or
// projector domain consumes them; see sdk/go/factschema/workitem/v1/README.md).
// It moved here from the query root's factschema_decode_workitem.go (#6642);
// package query's factschema_decode_shared.go keeps queryDecodeError,
// newQueryDecodeError, and queryDefaultSchemaMajorVersion because
// factschema_decode_supplychain.go still needs them, so this package calls
// decode.Error/decode.New directly instead of the root wrapper, and
// carries its own defaultSchemaMajorVersion rather than importing root for a
// one-line literal.
//
// Each wraps the contracts-module Decode* seam and, on a classified
// *factschema.DecodeError (a missing/null required identity field), returns a
// *decode.Error so the caller can route it to an input_invalid-class
// read-model outcome (a dropped row, not an empty-identity row) instead of
// silently defaulting every field to "".

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

// defaultSchemaMajorVersion is the schema version this package assumes when a
// row carries none. It is a major-1 version because every migrated work_item
// fact kind is at schema major 1 today; the Decode seam dispatches on the
// major component only. This literal is intentionally duplicated from root's
// queryDefaultSchemaMajorVersion (factschema_decode_shared.go) rather than
// imported: this package must not import package query, and the value is a
// one-line constant, not logic worth hoisting into querycontract for two
// callers (#6642).
const defaultSchemaMajorVersion = "1.0.0"

// workItemSchemaEnvelope adapts one scanned work-item fact row into the
// contracts-module factschema.Envelope the Decode* seam accepts. An empty
// schemaVersion is normalized to the current major-1 schema version — every
// Jira work-item emitter stamps a concrete "1.0.0" version
// (facts.WorkItemSchemaVersionV1), so a version-less row does not occur on the
// production path; a present but unsupported major still dead-letters through
// the Decode* seam's default branch.
func workItemSchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = defaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// decodeWorkItemRecord decodes one work_item.record fact row into the typed
// struct through the contracts seam. A missing required field
// (provider_work_item_id, work_item_key) yields a self-classifying
// *decode.Error.
func decodeWorkItemRecord(in workItemDecodeInput) (workitemv1.WorkItemRecord, error) {
	record, err := factschema.DecodeWorkItemRecord(workItemSchemaEnvelope(factschema.FactKindWorkItemRecord, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemRecord{}, decode.New(factschema.FactKindWorkItemRecord, in.FactID, err)
	}
	return record, nil
}

// decodeWorkItemTransition decodes one work_item.transition fact row into the
// typed struct. A missing required field (provider_changelog_id) yields a
// self-classifying *decode.Error.
func decodeWorkItemTransition(in workItemDecodeInput) (workitemv1.WorkItemTransition, error) {
	transition, err := factschema.DecodeWorkItemTransition(workItemSchemaEnvelope(factschema.FactKindWorkItemTransition, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemTransition{}, decode.New(factschema.FactKindWorkItemTransition, in.FactID, err)
	}
	return transition, nil
}

// decodeWorkItemExternalLink decodes one work_item.external_link fact row
// into the typed struct. Only "provider" is required for this kind (see
// workitem/v1/README.md), so this rarely dead-letters.
func decodeWorkItemExternalLink(in workItemDecodeInput) (workitemv1.WorkItemExternalLink, error) {
	link, err := factschema.DecodeWorkItemExternalLink(workItemSchemaEnvelope(factschema.FactKindWorkItemExternalLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemExternalLink{}, decode.New(factschema.FactKindWorkItemExternalLink, in.FactID, err)
	}
	return link, nil
}

// decodeWorkItemProjectMetadata decodes one work_item.project_metadata fact
// row into the typed struct. Only "provider" is required for this kind.
func decodeWorkItemProjectMetadata(in workItemDecodeInput) (workitemv1.WorkItemProjectMetadata, error) {
	metadata, err := factschema.DecodeWorkItemProjectMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemProjectMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemProjectMetadata{}, decode.New(factschema.FactKindWorkItemProjectMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemIssueTypeMetadata decodes one work_item.issue_type_metadata
// fact row into the typed struct. A missing required field (provider,
// issue_type_id) yields a self-classifying *decode.Error.
func decodeWorkItemIssueTypeMetadata(in workItemDecodeInput) (workitemv1.WorkItemIssueTypeMetadata, error) {
	metadata, err := factschema.DecodeWorkItemIssueTypeMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemIssueTypeMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemIssueTypeMetadata{}, decode.New(factschema.FactKindWorkItemIssueTypeMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemStatusMetadata decodes one work_item.status_metadata fact row
// into the typed struct. A missing required field (status_id) yields a
// self-classifying *decode.Error.
func decodeWorkItemStatusMetadata(in workItemDecodeInput) (workitemv1.WorkItemStatusMetadata, error) {
	metadata, err := factschema.DecodeWorkItemStatusMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemStatusMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemStatusMetadata{}, decode.New(factschema.FactKindWorkItemStatusMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemWorkflowMetadata decodes one work_item.workflow_metadata fact
// row into the typed struct. A missing required field (workflow_id) yields a
// self-classifying *decode.Error.
func decodeWorkItemWorkflowMetadata(in workItemDecodeInput) (workitemv1.WorkItemWorkflowMetadata, error) {
	metadata, err := factschema.DecodeWorkItemWorkflowMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemWorkflowMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemWorkflowMetadata{}, decode.New(factschema.FactKindWorkItemWorkflowMetadata, in.FactID, err)
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
		return workitemv1.WorkItemFieldMetadata{}, decode.New(factschema.FactKindWorkItemFieldMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemMetadataWarning decodes one work_item.metadata_warning fact
// row into the typed struct. A missing required field (metadata_type, reason)
// yields a self-classifying *decode.Error.
func decodeWorkItemMetadataWarning(in workItemDecodeInput) (workitemv1.WorkItemMetadataWarning, error) {
	warning, err := factschema.DecodeWorkItemMetadataWarning(workItemSchemaEnvelope(factschema.FactKindWorkItemMetadataWarning, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemMetadataWarning{}, decode.New(factschema.FactKindWorkItemMetadataWarning, in.FactID, err)
	}
	return warning, nil
}

// derefString returns the value a *string points at, or "" when it is nil,
// matching the pre-typing StringVal("") behavior for a field this migration
// converts from a raw payload lookup to a typed pointer.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// derefBool returns the value a *bool points at, or false when it is nil,
// matching the pre-typing BoolVal(false) behavior.
func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
