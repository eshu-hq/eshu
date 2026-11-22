// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factwrite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// OwnerConflictFailureClass is the closed failure class of a reducer write that
// found its fact id held by another scope (#11213). It equals
// telemetry.RefreshSkipReasonFactIDOwnerConflict, the class the collector save
// path uses for the same defect; a reducer test pins the equality.
const OwnerConflictFailureClass = "fact_id_owner_conflict"

// ownerSampleCap bounds how many conflicting facts an OwnerConflictError names.
const ownerSampleCap = 10

// Writer labels of an OwnerConflictError, the closed set of the `writer` label
// on eshu_dp_fact_id_scope_conflicts_total for the reducer writers.
const (
	WriterSingle    = "reducer_single"
	WriterBatch     = "reducer_batch"
	WriterVersioned = "reducer_versioned"
)

// OwnerConflictSample is one fact whose id another scope holds. Identifiers
// only, never a payload. StoredScopeID is empty when the writer could not look
// the row up.
type OwnerConflictSample struct {
	FactID               string
	FactKind             string
	IncomingScopeID      string
	IncomingGenerationID string
	StoredScopeID        string
}

// OwnerConflictError reports that a reducer fact write found a fact_id already
// held by another scope. The conflict clause skipped the row, so the owner row is
// unchanged; the write is refused rather than counted as done. It is not
// retryable (a collision is deterministic), so the reducer work item dead-letters
// with the failure class fact_id_owner_conflict instead of retrying forever.
type OwnerConflictError struct {
	// Writer is the closed writer label: WriterSingle, WriterBatch or WriterVersioned.
	Writer string
	// Count is the number of facts of the statement that were refused.
	Count int
	// Samples holds at most ownerSampleCap of them.
	Samples []OwnerConflictSample
}

// Error names the writer and the first conflicting fact; no payload.
func (e *OwnerConflictError) Error() string {
	first := ""
	if len(e.Samples) > 0 {
		s := e.Samples[0]
		first = fmt.Sprintf("; first fact %q (kind %q) from scope %q is held by scope %q", s.FactID, s.FactKind, s.IncomingScopeID, s.StoredScopeID)
	}
	return fmt.Sprintf("%s: %d reducer fact(s) refused by writer %s because their fact_id is held by another scope%s",
		OwnerConflictFailureClass, e.Count, e.Writer, first)
}

// Retryable is false: the collision repeats on every retry.
func (*OwnerConflictError) Retryable() bool { return false }

// FailureClass is the closed class the reducer failure path records.
func (*OwnerConflictError) FailureClass() string { return OwnerConflictFailureClass }

// ownerRow is the identity of one row of a statement, enough to name a conflict.
type ownerRow struct {
	FactID, ScopeID, GenerationID, FactKind string
}

// ownerHeld is a row of the statement whose fact_id another scope holds.
type ownerHeld struct {
	FactID, StoredScopeID string
}

// ownerProbe resolves which rows of a statement another scope holds.
type ownerProbe func(context.Context, []ownerRow) ([]ownerHeld, error)

// CheckOwned classifies a single-row SingleInsertQuery result. The statement's
// only conflict WHERE is the scope owner leg, so a row the statement skipped
// (zero rows affected) is held by another scope and is an OwnerConflictError.
// When db can query, the stored owner is looked up for the error; a failed lookup
// leaves it empty and never hides the conflict.
func CheckOwned(ctx context.Context, db Execer, result sql.Result, writer, factID, scopeID, generationID, factKind string) error {
	if result == nil {
		return nil // a recording fake that returns no result cannot report rows affected
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for reducer fact %q: %w", factID, err)
	}
	if affected > 0 {
		return nil
	}
	sample := OwnerConflictSample{FactID: factID, FactKind: factKind, IncomingScopeID: scopeID, IncomingGenerationID: generationID}
	if probe := probeFor(db); probe != nil {
		if held, probeErr := probe(ctx, []ownerRow{{FactID: factID, ScopeID: scopeID, GenerationID: generationID, FactKind: factKind}}); probeErr == nil && len(held) > 0 {
			sample.StoredScopeID = held[0].StoredScopeID
		}
	}
	return &OwnerConflictError{Writer: writer, Count: 1, Samples: []OwnerConflictSample{sample}}
}

// resolveShortfall classifies one batch statement. Rows landed in the number of
// rows affected; a shortfall is either a stale write the fencing predicate
// rightly skipped or a row another scope holds, and only the probe can tell
// them apart. A handle that cannot query (the recording fakes of the writers'
// own tests) cannot resolve it and reports nothing; every production handle is a
// db.Queryer. A failing probe is an error, never a clean batch.
func resolveShortfall(ctx context.Context, result sql.Result, writer string, rows []ownerRow, probe ownerProbe) error {
	if result == nil {
		return nil // a recording fake that returns no result cannot report rows affected
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected for %s batch: %w", writer, err)
	}
	if affected >= int64(len(rows)) || probe == nil {
		return nil
	}
	held, err := probe(ctx, rows)
	if err != nil {
		return fmt.Errorf("probe fact owners after a short %s batch: %w", writer, err)
	}
	if len(held) == 0 {
		return nil
	}
	byID := make(map[string]ownerRow, len(rows))
	for _, row := range rows {
		byID[row.FactID] = row
	}
	conflict := &OwnerConflictError{Writer: writer, Count: len(held)}
	for _, h := range held {
		if len(conflict.Samples) == ownerSampleCap {
			break
		}
		row := byID[h.FactID]
		conflict.Samples = append(conflict.Samples, OwnerConflictSample{
			FactID: h.FactID, FactKind: row.FactKind, IncomingScopeID: row.ScopeID,
			IncomingGenerationID: row.GenerationID, StoredScopeID: h.StoredScopeID,
		})
	}
	return conflict
}

// foreignOwnersQuery returns the stored owner of every incoming fact id that a
// different scope holds. A primary-key probe: it reads no payload.
const foreignOwnersQuery = `
SELECT t.fact_id, f.scope_id
FROM unnest($1::text[], $2::text[]) AS t(fact_id, scope_id)
JOIN fact_records AS f ON f.fact_id = t.fact_id
WHERE f.scope_id <> t.scope_id`

// probeFor returns the owner probe for db, or nil when db cannot query.
func probeFor(database Execer) ownerProbe {
	queryer, ok := database.(db.Queryer)
	if !ok {
		return nil
	}
	return func(ctx context.Context, rows []ownerRow) ([]ownerHeld, error) {
		factIDs := make([]string, len(rows))
		scopeIDs := make([]string, len(rows))
		for i, row := range rows {
			factIDs[i], scopeIDs[i] = row.FactID, row.ScopeID
		}
		cursor, err := queryer.QueryContext(ctx, foreignOwnersQuery, factIDs, scopeIDs)
		if err != nil {
			return nil, err
		}
		defer func() { _ = cursor.Close() }()
		var held []ownerHeld
		for cursor.Next() {
			var h ownerHeld
			if err := cursor.Scan(&h.FactID, &h.StoredScopeID); err != nil {
				return nil, err
			}
			held = append(held, h)
		}
		return held, cursor.Err()
	}
}

// ownerRowsOf names the rows of a plain chunk.
func ownerRowsOf(chunk []Row) []ownerRow {
	out := make([]ownerRow, len(chunk))
	for i, r := range chunk {
		out[i] = ownerRow{FactID: r.FactID, ScopeID: r.ScopeID, GenerationID: r.GenerationID, FactKind: r.FactKind}
	}
	return out
}

// ownerRowsOfVersioned names the rows of a versioned chunk.
func ownerRowsOfVersioned(chunk []VersionedRow) []ownerRow {
	out := make([]ownerRow, len(chunk))
	for i, r := range chunk {
		out[i] = ownerRow{FactID: r.FactID, ScopeID: r.ScopeID, GenerationID: r.GenerationID, FactKind: r.FactKind}
	}
	return out
}
