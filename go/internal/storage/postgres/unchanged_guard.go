// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Unchanged-row skip guard for issue #10873 (parent epic #10860).
//
// Saving a fact that is already stored rewrote its row on every save: both
// timestamps were stamped from the incoming copy and both are indexed, so the
// row moved even when nothing else changed. The upsertFactBatchSuffix WHERE
// clause now also requires at least one compared column to differ, fencing
// predicate (#4444) first and verbatim. A skipped row keeps the timestamps of
// its last real change; last-seen lives at generation grain
// (scope_generations/source states), never on the fact row.

// factGuardSkippedColumns are the upsertFactBatchSuffix SET columns the
// unchanged-row guard deliberately leaves out of its comparison:
// observed_at and ingested_at are both stamped from the incoming envelope on
// every save, so comparing them would stop the guard from ever skipping. A
// skipped row keeps the timestamps of its last real change.
var factGuardSkippedColumns = []string{"observed_at", "ingested_at"}

// factGuardKeyColumns are INSERT columns that are never updated: the fact_id
// conflict target and the scope/generation the row was born in.
var factGuardKeyColumns = []string{"fact_id", "scope_id", "generation_id"}

// guardedFactValues holds one envelope's upsertFactBatchSuffix guarded columns
// with the writer's normalization applied: schema_version, collector_kind,
// and source_confidence defaults plus NULL-able source_uri/source_record_id.
// buildUpsertFactBatchQuery and selectUnchangedFactIDs both read through this
// helper so the discriminator compares exactly what the writer stores; a
// normalization drift would misclassify unchanged rows as fenced out (starving
// derivation) or fenced rows as unchanged (leaking stale payloads).
type guardedFactValues struct {
	factKind         string
	stableFactKey    string
	schemaVersion    string
	collectorKind    string
	fencingToken     int64
	sourceConfidence string
	sourceSystem     string
	sourceFactKey    string
	sourceURI        any
	sourceRecordID   any
	isTombstone      bool
	payloadJSON      []byte
}

func guardedFactValuesFor(envelope facts.Envelope) (guardedFactValues, error) {
	payloadJSON, err := payloadstore.MarshalPayload(envelope.Payload)
	if err != nil {
		return guardedFactValues{}, fmt.Errorf("marshal payload for fact %q: %w", envelope.FactID, err)
	}

	return guardedFactValues{
		factKind:         envelope.FactKind,
		stableFactKey:    envelope.StableFactKey,
		schemaVersion:    payloadstore.EmptyToDefault(envelope.SchemaVersion, "0.0.0"),
		collectorKind:    payloadstore.EmptyToDefault(envelope.CollectorKind, payloadstore.EmptyToDefault(envelope.SourceRef.SourceSystem, "unknown")),
		fencingToken:     envelope.FencingToken,
		sourceConfidence: payloadstore.EmptyToDefault(envelope.SourceConfidence, "unknown"),
		sourceSystem:     envelope.SourceRef.SourceSystem,
		sourceFactKey:    envelope.SourceRef.FactKey,
		sourceURI:        payloadstore.EmptyToNil(envelope.SourceRef.SourceURI),
		sourceRecordID:   payloadstore.EmptyToNil(envelope.SourceRef.SourceRecordID),
		isTombstone:      envelope.IsTombstone,
		payloadJSON:      payloadJSON,
	}, nil
}

// factGuardCompareColumns is the number of values per row in the
// selectUnchangedFactIDs VALUES list: the fact_id join key, the incoming scope
// and generation (#11203), plus the 12 guarded columns the unchanged-row guard
// compares.
const factGuardCompareColumns = 15

// unchangedSplit is the three-way split of a batch's RETURNING-absent
// fact_ids (issue #11203): rows that already equal the incoming envelope
// (Unchanged: admitted for derivation), rows another scope or generation holds
// (Conflicts: the owner guard skipped them), and, by omission from both, rows
// the fencing guard lost to a higher stored token (fenced out, excluded).
type unchangedSplit struct {
	Unchanged map[string]struct{}
	Conflicts []factIDOwnerConflict
}

// selectUnchangedFactIDs classifies the candidates the guarded upsert did not
// return, in one statement on the caller's transaction right after the upsert,
// so it sees the upsert's own effects. A candidate whose stored scope or
// generation differs from the incoming one is an owner conflict whatever its
// content: it must never read as "unchanged", or a foreign write whose guarded
// columns and fencing token equal the owner's would be admitted for derivation
// (#11203). Among rows the incoming scope and generation own, one whose stored
// row equals the incoming envelope on every guarded column, fencing token
// included, is unchanged; the rest were fenced out. A row a concurrent batch
// changed in between no longer matches and stays excluded, which is the
// conservative direction (under-admission re-derives nothing stale:
// derivation is a pure function of envelope content the winning batch carries
// itself). A higher-token same-content write is never reported here: its token
// differs from the stored one, so the upsert rewrote the row and RETURNING
// already accepted it.
func selectUnchangedFactIDs(
	ctx context.Context,
	database db.ExecQueryer,
	candidates []facts.Envelope,
) (unchangedSplit, error) {
	split := unchangedSplit{Unchanged: make(map[string]struct{}, len(candidates))}
	if len(candidates) == 0 {
		return split, nil
	}

	var rows strings.Builder
	args := make([]any, 0, len(candidates)*factGuardCompareColumns)
	byID := make(map[string]facts.Envelope, len(candidates))
	for i, envelope := range candidates {
		guarded, err := guardedFactValuesFor(envelope)
		if err != nil {
			return unchangedSplit{}, err
		}
		byID[envelope.FactID] = envelope
		if i > 0 {
			rows.WriteString(", ")
		}
		offset := i * factGuardCompareColumns
		fmt.Fprintf(
			&rows,
			"($%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::bigint, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::boolean, $%d::jsonb)",
			offset+1, offset+2, offset+3, offset+4, offset+5,
			offset+6, offset+7, offset+8, offset+9, offset+10,
			offset+11, offset+12, offset+13, offset+14, offset+15,
		)
		args = append(
			args,
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			guarded.factKind,
			guarded.stableFactKey,
			guarded.schemaVersion,
			guarded.collectorKind,
			guarded.fencingToken,
			guarded.sourceConfidence,
			guarded.sourceSystem,
			guarded.sourceFactKey,
			guarded.sourceURI,
			guarded.sourceRecordID,
			guarded.isTombstone,
			guarded.payloadJSON,
		)
	}

	query := `SELECT v.fact_id, f.scope_id, f.generation_id,
  (f.fact_kind IS NOT DISTINCT FROM v.fact_kind
  AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key
  AND f.schema_version IS NOT DISTINCT FROM v.schema_version
  AND f.collector_kind IS NOT DISTINCT FROM v.collector_kind
  AND f.fencing_token IS NOT DISTINCT FROM v.fencing_token
  AND f.source_confidence IS NOT DISTINCT FROM v.source_confidence
  AND f.source_system IS NOT DISTINCT FROM v.source_system
  AND f.source_fact_key IS NOT DISTINCT FROM v.source_fact_key
  AND f.source_uri IS NOT DISTINCT FROM v.source_uri
  AND f.source_record_id IS NOT DISTINCT FROM v.source_record_id
  AND f.is_tombstone IS NOT DISTINCT FROM v.is_tombstone
  AND f.payload IS NOT DISTINCT FROM v.payload) AS unchanged
FROM (VALUES ` + rows.String() + `) AS v(
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
    collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
    source_uri, source_record_id, is_tombstone, payload
)
JOIN fact_records AS f ON f.fact_id = v.fact_id`

	result, err := database.QueryContext(ctx, query, args...)
	if err != nil {
		return unchangedSplit{}, fmt.Errorf("classify rejected fact ids (%d facts): %w", len(candidates), err)
	}
	defer func() { _ = result.Close() }()

	for result.Next() {
		var factID, storedScope, storedGeneration string
		var unchanged bool
		if err := result.Scan(&factID, &storedScope, &storedGeneration, &unchanged); err != nil {
			return unchangedSplit{}, fmt.Errorf("scan rejected fact_id: %w", err)
		}
		incoming := byID[factID]
		switch {
		case storedScope != incoming.ScopeID || storedGeneration != incoming.GenerationID:
			split.Conflicts = append(split.Conflicts, factIDOwnerConflict{
				FactID:               factID,
				FactKind:             incoming.FactKind,
				IncomingScopeID:      incoming.ScopeID,
				IncomingGenerationID: incoming.GenerationID,
				StoredScopeID:        storedScope,
				StoredGenerationID:   storedGeneration,
			})
		case unchanged:
			split.Unchanged[factID] = struct{}{}
		}
	}
	if err := result.Err(); err != nil {
		return unchangedSplit{}, fmt.Errorf("classify rejected fact ids rows: %w", err)
	}

	return split, nil
}
