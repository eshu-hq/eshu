// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ScanFactEnvelope scans one fact_records row into an Envelope. The column
// order must match the fact loaders' SELECT lists: fact_id, scope_id,
// generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
// fencing_token, source_confidence, source_system, source_fact_key,
// source_uri, source_record_id, observed_at, is_tombstone, payload. It lives
// in the engine package because the deferred-backfill fact loaders are its
// primary consumers; the root fact families share it through this seam so the
// row shape has one home (issue #7794).
func ScanFactEnvelope(rows db.Rows) (facts.Envelope, error) {
	var factID string
	var scopeID string
	var generationID string
	var factKind string
	var stableFactKey string
	var schemaVersion string
	var collectorKind string
	var fencingToken int64
	var sourceConfidence string
	var sourceSystem string
	var sourceFactKey string
	var sourceURI string
	var sourceRecordID string
	var observedAt time.Time
	var isTombstone bool
	var rawPayload []byte

	if err := rows.Scan(
		&factID,
		&scopeID,
		&generationID,
		&factKind,
		&stableFactKey,
		&schemaVersion,
		&collectorKind,
		&fencingToken,
		&sourceConfidence,
		&sourceSystem,
		&sourceFactKey,
		&sourceURI,
		&sourceRecordID,
		&observedAt,
		&isTombstone,
		&rawPayload,
	); err != nil {
		return facts.Envelope{}, err
	}

	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return facts.Envelope{}, err
	}

	return facts.Envelope{
		FactID:           factID,
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    schemaVersion,
		CollectorKind:    collectorKind,
		FencingToken:     fencingToken,
		SourceConfidence: sourceConfidence,
		ObservedAt:       observedAt.UTC(),
		Payload:          payload,
		IsTombstone:      isTombstone,
		SourceRef: facts.Ref{
			SourceSystem:   sourceSystem,
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        sourceFactKey,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}, nil
}
