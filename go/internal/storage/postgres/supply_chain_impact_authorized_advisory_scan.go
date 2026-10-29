// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

type authorizedImpactAdvisoryFact struct {
	envelope   facts.Envelope
	payloadSHA string
}

func scanAuthorizedImpactAdvisoryFact(rows db.Rows) (authorizedImpactAdvisoryFact, error) {
	var row authorizedImpactAdvisoryFact
	var sourceSystem, sourceFactKey, sourceURI, sourceRecordID string
	var rawPayload []byte
	envelope := &row.envelope
	if err := rows.Scan(
		&envelope.FactID, &envelope.ScopeID, &envelope.GenerationID,
		&envelope.FactKind, &envelope.StableFactKey, &envelope.SchemaVersion,
		&envelope.CollectorKind, &envelope.FencingToken, &envelope.SourceConfidence,
		&sourceSystem, &sourceFactKey, &sourceURI, &sourceRecordID,
		&envelope.ObservedAt, &envelope.IsTombstone, &rawPayload, &row.payloadSHA,
	); err != nil {
		return authorizedImpactAdvisoryFact{}, err
	}
	payload, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return authorizedImpactAdvisoryFact{}, err
	}
	envelope.Payload = payload
	envelope.SourceRef = facts.Ref{
		SourceSystem: sourceSystem, ScopeID: envelope.ScopeID,
		GenerationID: envelope.GenerationID, FactKey: sourceFactKey,
		SourceURI: sourceURI, SourceRecordID: sourceRecordID,
	}
	return row, nil
}
