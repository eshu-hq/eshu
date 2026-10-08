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
// It moved here from internal/query/workitem/factschema_decode.go (#6623),
// absorbing the verbatim fork internal/query/incident/store/decode_workitem.go
// carried, so both read paths execute this code. It calls decode.Error,
// decode.New, decode.DefaultSchemaMajorVersion and decode.DerefString
// directly.
//
// Each wraps the contracts-module Decode* seam and, on a classified
// *factschema.DecodeError (a missing/null required identity field), returns a
// *decode.Error so the caller can route it to an input_invalid-class
// read-model outcome (a dropped row, not an empty-identity row) instead of
// silently defaulting every field to "".

// DecodeInput carries one scanned work-item fact row into a decode
// wrapper. Bundling the row's identity, schema version, and payload into a
// single parameter lets each decode wrapper keep the one-argument shape the
// payload-usage manifest gate's seam parser recognizes (a decode<Kind> func
// taking one value and returning (workitemv1.<Struct>, error)); a three-arg
// wrapper would be invisible to the gate, leaving the query decode sites
// silently ungated. FactID is retained only for operator-facing error
// attribution, never for decode input.
type DecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// SchemaEnvelope adapts one scanned fact row into the
// contracts-module factschema.Envelope the Decode* seam accepts. An empty
// schemaVersion is normalized to the current major-1 schema version — every
// Jira work-item emitter stamps a concrete "1.0.0" version
// (facts.WorkItemSchemaVersionV1), so a version-less row does not occur on the
// production path; a present but unsupported major still dead-letters through
// the Decode* seam's default branch.
//
// Naming debt (#6623): the incident store also calls this for incident kinds
// (it did so through its fork before the move). The helper is generic —
// kind-agnostic version normalization — but keeps its work-item home because
// the work-item wrappers are its primary consumers.
func SchemaEnvelope(factKind, schemaVersion string, payload map[string]any) factschema.Envelope {
	if schemaVersion == "" {
		schemaVersion = decode.DefaultSchemaMajorVersion
	}
	return factschema.Envelope{
		FactKind:      factKind,
		SchemaVersion: schemaVersion,
		Payload:       payload,
	}
}

// DecodeRecord decodes one work_item.record fact row into the typed
// struct through the contracts seam. A missing required field
// (provider_work_item_id, work_item_key) yields a self-classifying
// *decode.Error.
func DecodeRecord(in DecodeInput) (workitemv1.WorkItemRecord, error) {
	record, err := factschema.DecodeWorkItemRecord(SchemaEnvelope(factschema.FactKindWorkItemRecord, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemRecord{}, decode.New(factschema.FactKindWorkItemRecord, in.FactID, err)
	}
	return record, nil
}

// DecodeTransition decodes one work_item.transition fact row into the
// typed struct. A missing required field (provider_changelog_id) yields a
// self-classifying *decode.Error.
func DecodeTransition(in DecodeInput) (workitemv1.WorkItemTransition, error) {
	transition, err := factschema.DecodeWorkItemTransition(SchemaEnvelope(factschema.FactKindWorkItemTransition, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemTransition{}, decode.New(factschema.FactKindWorkItemTransition, in.FactID, err)
	}
	return transition, nil
}

// DecodeExternalLink decodes one work_item.external_link fact row
// into the typed struct. Only "provider" is required for this kind (see
// workitem/v1/README.md), so this rarely dead-letters.
func DecodeExternalLink(in DecodeInput) (workitemv1.WorkItemExternalLink, error) {
	link, err := factschema.DecodeWorkItemExternalLink(SchemaEnvelope(factschema.FactKindWorkItemExternalLink, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemExternalLink{}, decode.New(factschema.FactKindWorkItemExternalLink, in.FactID, err)
	}
	return link, nil
}

// DecodeProjectMetadata decodes one work_item.project_metadata fact
// row into the typed struct. Only "provider" is required for this kind.
func DecodeProjectMetadata(in DecodeInput) (workitemv1.WorkItemProjectMetadata, error) {
	metadata, err := factschema.DecodeWorkItemProjectMetadata(SchemaEnvelope(factschema.FactKindWorkItemProjectMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemProjectMetadata{}, decode.New(factschema.FactKindWorkItemProjectMetadata, in.FactID, err)
	}
	return metadata, nil
}

// DecodeIssueTypeMetadata decodes one work_item.issue_type_metadata
// fact row into the typed struct. A missing required field (provider,
// issue_type_id) yields a self-classifying *decode.Error.
func DecodeIssueTypeMetadata(in DecodeInput) (workitemv1.WorkItemIssueTypeMetadata, error) {
	metadata, err := factschema.DecodeWorkItemIssueTypeMetadata(SchemaEnvelope(factschema.FactKindWorkItemIssueTypeMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemIssueTypeMetadata{}, decode.New(factschema.FactKindWorkItemIssueTypeMetadata, in.FactID, err)
	}
	return metadata, nil
}

// DecodeStatusMetadata decodes one work_item.status_metadata fact row
// into the typed struct. A missing required field (status_id) yields a
// self-classifying *decode.Error.
func DecodeStatusMetadata(in DecodeInput) (workitemv1.WorkItemStatusMetadata, error) {
	metadata, err := factschema.DecodeWorkItemStatusMetadata(SchemaEnvelope(factschema.FactKindWorkItemStatusMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemStatusMetadata{}, decode.New(factschema.FactKindWorkItemStatusMetadata, in.FactID, err)
	}
	return metadata, nil
}

// DecodeWorkflowMetadata decodes one work_item.workflow_metadata fact
// row into the typed struct. A missing required field (workflow_id) yields a
// self-classifying *decode.Error.
func DecodeWorkflowMetadata(in DecodeInput) (workitemv1.WorkItemWorkflowMetadata, error) {
	metadata, err := factschema.DecodeWorkItemWorkflowMetadata(SchemaEnvelope(factschema.FactKindWorkItemWorkflowMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemWorkflowMetadata{}, decode.New(factschema.FactKindWorkItemWorkflowMetadata, in.FactID, err)
	}
	return metadata, nil
}

// DecodeFieldMetadata decodes one work_item.field_metadata fact row
// into the typed struct. Only "provider" is required for this kind — the
// payload's own field_id is always redacted to "" by the collector (see
// workitem/v1/README.md), so this rarely dead-letters.
func DecodeFieldMetadata(in DecodeInput) (workitemv1.WorkItemFieldMetadata, error) {
	metadata, err := factschema.DecodeWorkItemFieldMetadata(SchemaEnvelope(factschema.FactKindWorkItemFieldMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemFieldMetadata{}, decode.New(factschema.FactKindWorkItemFieldMetadata, in.FactID, err)
	}
	return metadata, nil
}

// DecodeMetadataWarning decodes one work_item.metadata_warning fact
// row into the typed struct. A missing required field (metadata_type, reason)
// yields a self-classifying *decode.Error.
func DecodeMetadataWarning(in DecodeInput) (workitemv1.WorkItemMetadataWarning, error) {
	warning, err := factschema.DecodeWorkItemMetadataWarning(SchemaEnvelope(factschema.FactKindWorkItemMetadataWarning, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemMetadataWarning{}, decode.New(factschema.FactKindWorkItemMetadataWarning, in.FactID, err)
	}
	return warning, nil
}

// DerefBool returns the value a *bool points at, or false when it is nil,
// matching the pre-typing BoolVal(false) behavior.
func DerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}
