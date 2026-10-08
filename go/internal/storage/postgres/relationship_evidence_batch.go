// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
)

// evidenceInsertColumns is the number of columns bound per evidence row in the
// relationship_evidence_facts insert. It pairs with evidenceInsertBatchRows to
// size the multi-row INSERT placeholder list.
const evidenceInsertColumns = 12

// evidenceInsertBatchRows bounds how many evidence rows one multi-row INSERT
// statement carries. Each backfill evidence row is small and the insert is
// `ON CONFLICT (evidence_id) DO NOTHING`, so batching trades a few large
// statements for the per-row round-trips that dominated the deferred backfill
// long pole (issue #3704): one ExecContext per fact became one ExecContext per
// 500 facts. 500 matches the fact-write batch size (FactStore upserts 500 rows)
// and stays well under PostgreSQL's 65535 bound-parameter limit
// (500 * 12 = 6000 parameters).
const evidenceInsertBatchRows = 500

// insertEvidenceFactBatch builds and executes one multi-row evidence INSERT for
// the supplied slice. The per-row evidence_id digest and column binding match the
// prior single-row path exactly, so batching changes only the number of
// round-trips, not the rows written. It returns the rows the statement
// inserted: the ON CONFLICT (evidence_id) DO NOTHING clause skips
// already-committed rows without counting them, so a re-upsert of identical
// evidence reports 0.
func (s *RelationshipStore) insertEvidenceFactBatch(
	ctx context.Context,
	generationID string,
	facts []relationships.EvidenceFact,
	now time.Time,
) (int64, error) {
	if len(facts) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString(insertEvidenceFactBatchPrefix)
	args := make([]any, 0, len(facts)*evidenceInsertColumns)
	for i, f := range facts {
		detailsJSON, err := json.Marshal(f.Details)
		if err != nil {
			return 0, fmt.Errorf("marshal evidence details: %w", err)
		}
		evidenceID := relationshipDigest(
			"evidence",
			generationID,
			string(f.EvidenceKind),
			string(f.RelationshipType),
			f.SourceRepoID,
			f.TargetRepoID,
			f.SourceEntityID,
			f.TargetEntityID,
			fmt.Sprintf("%.12g", f.Confidence),
			f.Rationale,
			string(detailsJSON),
		)
		if i > 0 {
			sb.WriteString(", ")
		}
		base := i * evidenceInsertColumns
		sb.WriteString(evidenceRowPlaceholders(base))
		args = append(
			args,
			evidenceID,
			generationID,
			string(f.EvidenceKind),
			string(f.RelationshipType),
			payloadstore.EmptyToNil(f.SourceRepoID),
			payloadstore.EmptyToNil(f.TargetRepoID),
			payloadstore.EmptyToNil(f.SourceEntityID),
			payloadstore.EmptyToNil(f.TargetEntityID),
			f.Confidence,
			f.Rationale,
			detailsJSON,
			now,
		)
	}
	sb.WriteString(insertEvidenceFactBatchSuffix)

	result, err := s.database.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("insert evidence fact batch (%d rows): %w", len(facts), err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("insert evidence fact batch (%d rows) rows affected: %w", len(facts), err)
	}
	return inserted, nil
}

// UpsertEvidenceFacts persists evidence facts for a generation in bounded
// multi-row INSERT batches. Each batch is one idempotent
// `INSERT ... ON CONFLICT (evidence_id) DO NOTHING` statement, so re-running the
// backfill converges to the same rows and the per-row round-trips that made the
// corpus-wide backfill the client-side long pole (issue #3704) are gone. Row
// identity (evidence_id) is unchanged, so the persisted evidence is byte-identical
// to the prior per-row path.
func (s *RelationshipStore) UpsertEvidenceFacts(
	ctx context.Context,
	generationID string,
	facts []relationships.EvidenceFact,
) error {
	_, err := s.UpsertEvidenceFactsCounted(ctx, generationID, facts)
	return err
}

// UpsertEvidenceFactsCounted persists evidence facts for a generation exactly
// like UpsertEvidenceFacts and additionally returns the number of rows the
// backfill inserted. The `ON CONFLICT (evidence_id) DO NOTHING` clause skips
// already-committed rows without counting them, so a re-upsert of identical
// evidence reports 0. The deferred relationship maintenance pass uses the count
// to distinguish "new evidence arrived" (reopen the partition's relationship
// items) from "already committed" (keep the memo-hit skip): only rows actually
// inserted this call can invalidate a skip-set entry computed at pass start.
func (s *RelationshipStore) UpsertEvidenceFactsCounted(
	ctx context.Context,
	generationID string,
	facts []relationships.EvidenceFact,
) (int64, error) {
	if len(facts) == 0 {
		return 0, nil
	}
	facts = relationships.DedupeEvidenceFacts(facts)
	if len(facts) == 0 {
		return 0, nil
	}

	var inserted int64
	now := time.Now().UTC()
	for start := 0; start < len(facts); start += evidenceInsertBatchRows {
		end := start + evidenceInsertBatchRows
		if end > len(facts) {
			end = len(facts)
		}
		n, err := s.insertEvidenceFactBatch(ctx, generationID, facts[start:end], now)
		if err != nil {
			return 0, err
		}
		inserted += n
	}
	return inserted, nil
}

// evidenceRowPlaceholders returns the `($base+1, ..., $base+12)` placeholder tuple
// for one evidence row in a multi-row INSERT, offset by the row's base parameter
// index.
func evidenceRowPlaceholders(base int) string {
	var sb strings.Builder
	sb.WriteByte('(')
	for c := 0; c < evidenceInsertColumns; c++ {
		if c > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(base + c + 1))
	}
	sb.WriteByte(')')
	return sb.String()
}
