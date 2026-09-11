// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"errors"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/decode"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	workitemv1 "github.com/eshu-hq/eshu/sdk/go/factschema/workitem/v1"
)

// This file forks the small work-item decode substrate the incident review
// evidence reads need (decodeWorkItemRecord, decodeWorkItemProjectMetadata,
// decodeWorkItemStatusMetadata, workItemDecodeInput, workItemSchemaEnvelope,
// newQueryDecodeError, workItemDerefString, workItemDerefBool, and
// logWorkItemEvidenceDecodeDrop) from the query root's
// factschema_decode_workitem.go and work_item_evidence.go, verbatim apart
// from the package clause. The substrate cannot be imported: it lives in
// package query, which this package must not import back without a cycle
// through the root compatibility aliases. It cannot move either: the
// work-item evidence family and the supply-chain and package-registry
// readers share it, and their lanes own that relocation. Each fork cites its
// source above its declaration; when the work-item lane promotes this
// substrate to a shared home, this file adopts that home and the forks go
// away. See #6060.

// workItemDecodeInput carries one scanned work-item fact row into a decode
// wrapper. Forked from workItemDecodeInput
// (internal/query/factschema_decode_workitem.go): bundling the row's
// identity, schema version, and payload into a single parameter keeps the
// one-argument shape the payload-usage manifest gate's seam parser
// recognizes. FactID is retained only for operator-facing error attribution,
// never for decode input.
type workItemDecodeInput struct {
	FactID        string
	SchemaVersion string
	Payload       map[string]any
}

// decodeWorkItemRecord decodes one work_item.record fact row into the typed
// struct through the contracts seam. Forked from decodeWorkItemRecord
// (internal/query/factschema_decode_workitem.go): a missing required field
// (provider_work_item_id, work_item_key) yields a self-classifying
// *decode.Error.
func decodeWorkItemRecord(in workItemDecodeInput) (workitemv1.WorkItemRecord, error) {
	record, err := factschema.DecodeWorkItemRecord(workItemSchemaEnvelope(factschema.FactKindWorkItemRecord, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemRecord{}, newQueryDecodeError(factschema.FactKindWorkItemRecord, in.FactID, err)
	}
	return record, nil
}

// decodeWorkItemProjectMetadata decodes one work_item.project_metadata fact
// row into the typed struct. Forked from decodeWorkItemProjectMetadata
// (internal/query/factschema_decode_workitem.go): a missing required field
// yields a self-classifying *decode.Error.
func decodeWorkItemProjectMetadata(in workItemDecodeInput) (workitemv1.WorkItemProjectMetadata, error) {
	metadata, err := factschema.DecodeWorkItemProjectMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemProjectMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemProjectMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemProjectMetadata, in.FactID, err)
	}
	return metadata, nil
}

// decodeWorkItemStatusMetadata decodes one work_item.status_metadata fact
// row into the typed struct. Forked from decodeWorkItemStatusMetadata
// (internal/query/factschema_decode_workitem.go): a missing required
// status_id anchor yields a self-classifying *decode.Error.
func decodeWorkItemStatusMetadata(in workItemDecodeInput) (workitemv1.WorkItemStatusMetadata, error) {
	metadata, err := factschema.DecodeWorkItemStatusMetadata(workItemSchemaEnvelope(factschema.FactKindWorkItemStatusMetadata, in.SchemaVersion, in.Payload))
	if err != nil {
		return workitemv1.WorkItemStatusMetadata{}, newQueryDecodeError(factschema.FactKindWorkItemStatusMetadata, in.FactID, err)
	}
	return metadata, nil
}

// newQueryDecodeError wraps a decode error returned by a factschema Decode*
// function into the query layer's classified decode failure. Forked from
// newQueryDecodeError (internal/query/factschema_decode_workitem.go).
func newQueryDecodeError(factKind, factID string, err error) *decode.Error {
	return decode.New(factKind, factID, err)
}

// workItemSchemaEnvelope adapts one scanned work-item fact row into the
// contracts-module factschema.Envelope the Decode* seam accepts. Forked from
// workItemSchemaEnvelope (internal/query/factschema_decode_workitem.go): an
// empty schemaVersion is normalized to the current major-1 schema version.
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
// when a row carries none. Forked from queryDefaultSchemaMajorVersion
// (internal/query/factschema_decode_workitem.go): a major-1 version because
// every migrated work_item fact kind is at schema major 1 today.
const queryDefaultSchemaMajorVersion = "1.0.0"

// workItemDerefString returns the value a *string points at, or "" when it
// is nil. Forked from workItemDerefString
// (internal/query/factschema_decode_workitem.go).
func workItemDerefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// workItemDerefBool returns the value a *bool points at, or false when it is
// nil, matching the pre-typing BoolVal(false) behavior. Forked from
// workItemDerefBool (internal/query/factschema_decode_workitem.go).
func workItemDerefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

// logWorkItemEvidenceDecodeDrop emits an operator-diagnosable debug log for
// a work-item evidence fact dropped from a read because its payload failed
// typed decode. Forked from logWorkItemEvidenceDecodeDrop
// (internal/query/work_item_evidence.go).
func logWorkItemEvidenceDecodeDrop(err error) {
	var decodeErr *decode.Error
	if !errors.As(err, &decodeErr) {
		slog.Debug("work-item evidence fact dropped from list: decode error", slog.String("error", err.Error()))
		return
	}
	attrs := []any{
		slog.String("fact_id", decodeErr.FactID),
		slog.String("fact_kind", decodeErr.FactKind),
		slog.String("classification", decodeErr.Classification),
	}
	if decodeErr.Field != "" {
		attrs = append(attrs, slog.String("missing_field", decodeErr.Field))
	}
	slog.Debug("work-item evidence fact dropped from list", attrs...)
}
