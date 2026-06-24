package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
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
// round-trips, not the rows written.
func (s *RelationshipStore) insertEvidenceFactBatch(
	ctx context.Context,
	generationID string,
	facts []relationships.EvidenceFact,
	now time.Time,
) error {
	if len(facts) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(insertEvidenceFactBatchPrefix)
	args := make([]any, 0, len(facts)*evidenceInsertColumns)
	for i, f := range facts {
		detailsJSON, err := json.Marshal(f.Details)
		if err != nil {
			return fmt.Errorf("marshal evidence details: %w", err)
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
			emptyToNil(f.SourceRepoID),
			emptyToNil(f.TargetRepoID),
			emptyToNil(f.SourceEntityID),
			emptyToNil(f.TargetEntityID),
			f.Confidence,
			f.Rationale,
			detailsJSON,
			now,
		)
	}
	sb.WriteString(insertEvidenceFactBatchSuffix)

	if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("insert evidence fact batch (%d rows): %w", len(facts), err)
	}
	return nil
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
