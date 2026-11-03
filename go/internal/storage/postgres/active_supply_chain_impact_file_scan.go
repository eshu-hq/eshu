// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// scanTypedSizedSupplyChainImpactFact is scanSizedSupplyChainImpactFact with
// the payload decoded through the typed file-payload decode (#9861): the
// envelope carries only the keys the reachability readers consume, while the
// returned size stays the raw payload bytes so budget accounting is
// unchanged. Order, ID, and error behavior match the map path: it errors only
// on a scan failure or malformed JSON, never on a shape mismatch.
func scanTypedSizedSupplyChainImpactFact(rows db.Rows) (facts.Envelope, int, error) {
	var envelope facts.Envelope
	var sourceSystem, sourceFactKey, sourceURI, sourceRecordID string
	var rawPayload []byte
	if err := rows.Scan(
		&envelope.FactID, &envelope.ScopeID, &envelope.GenerationID,
		&envelope.FactKind, &envelope.StableFactKey, &envelope.SchemaVersion,
		&envelope.CollectorKind, &envelope.FencingToken, &envelope.SourceConfidence,
		&sourceSystem, &sourceFactKey, &sourceURI, &sourceRecordID,
		&envelope.ObservedAt, &envelope.IsTombstone, &rawPayload,
	); err != nil {
		return facts.Envelope{}, 0, err
	}
	typed, err := supplychaincore.DecodeTypedFileEnvelope(rawPayload, envelope.FactID, envelope.ScopeID, envelope.IsTombstone)
	if err != nil {
		return facts.Envelope{}, 0, err
	}
	typed.FactKind = envelope.FactKind
	typed.GenerationID = envelope.GenerationID
	typed.StableFactKey = envelope.StableFactKey
	typed.SchemaVersion = envelope.SchemaVersion
	typed.CollectorKind = envelope.CollectorKind
	typed.FencingToken = envelope.FencingToken
	typed.SourceConfidence = envelope.SourceConfidence
	typed.ObservedAt = envelope.ObservedAt
	typed.SourceRef = facts.Ref{
		SourceSystem: sourceSystem,
		ScopeID:      envelope.ScopeID, GenerationID: envelope.GenerationID,
		FactKey: sourceFactKey, SourceURI: sourceURI, SourceRecordID: sourceRecordID,
	}
	return typed, len(rawPayload), nil
}
