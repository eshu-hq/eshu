// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

const relationshipReferenceCandidateKeyBatchSize = 500

const columnsPerRelationshipReferenceCandidateKeyRow = 5

type relationshipReferenceCandidateKeyRow struct {
	FactID       string
	ScopeID      string
	GenerationID string
	SourceRepoID string
	ReferenceKey string
}

func relationshipReferenceCandidateKeyRows(envelopes []facts.Envelope) []relationshipReferenceCandidateKeyRow {
	if len(envelopes) == 0 {
		return nil
	}

	rows := make([]relationshipReferenceCandidateKeyRow, 0, len(envelopes))
	for _, envelope := range envelopes {
		if envelope.IsTombstone || !relationshipReferenceFactKind(envelope.FactKind) {
			continue
		}
		payloadJSON, err := marshalPayload(envelope.Payload)
		if err != nil {
			continue
		}
		referenceKey := relationships.CatalogReferenceTokenStream(string(payloadJSON))
		if referenceKey == "" {
			continue
		}
		rows = append(rows, relationshipReferenceCandidateKeyRow{
			FactID:       envelope.FactID,
			ScopeID:      envelope.ScopeID,
			GenerationID: envelope.GenerationID,
			SourceRepoID: relationshipReferenceSourceRepoID(envelope),
			ReferenceKey: referenceKey,
		})
	}
	return rows
}

func relationshipReferenceFactKind(factKind string) bool {
	switch factKind {
	case "content", "file", facts.GCPCloudRelationshipFactKind:
		return true
	default:
		return false
	}
}

func relationshipReferenceSourceRepoID(envelope facts.Envelope) string {
	if repoID, _ := envelope.Payload["repo_id"].(string); strings.TrimSpace(repoID) != "" {
		return strings.ToLower(strings.TrimSpace(repoID))
	}
	return strings.ToLower(strings.TrimSpace(deferredScopedFactOwnRepoIDFromScope(envelope.ScopeID)))
}

func refreshRelationshipReferenceCandidateKeys(
	ctx context.Context,
	db ExecQueryer,
	envelopes []facts.Envelope,
) error {
	if db == nil || len(envelopes) == 0 {
		return nil
	}

	factIDs := acceptedFactIDs(envelopes)
	if len(factIDs) == 0 {
		return nil
	}
	if _, err := db.ExecContext(ctx, deleteRelationshipReferenceCandidateKeysSQL, pq.StringArray(factIDs)); err != nil {
		return fmt.Errorf("delete relationship reference candidate keys: %w", err)
	}

	rows := relationshipReferenceCandidateKeyRows(envelopes)
	if len(rows) == 0 {
		return nil
	}
	for start := 0; start < len(rows); start += relationshipReferenceCandidateKeyBatchSize {
		end := start + relationshipReferenceCandidateKeyBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertRelationshipReferenceCandidateKeyBatch(ctx, db, rows[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func insertRelationshipReferenceCandidateKeyBatch(
	ctx context.Context,
	db ExecQueryer,
	rows []relationshipReferenceCandidateKeyRow,
) error {
	if len(rows) == 0 {
		return nil
	}
	args := make([]any, 0, len(rows)*columnsPerRelationshipReferenceCandidateKeyRow)
	var values strings.Builder
	for i, row := range rows {
		if i > 0 {
			values.WriteString(", ")
		}
		offset := i * columnsPerRelationshipReferenceCandidateKeyRow
		fmt.Fprintf(
			&values,
			"($%d, $%d, $%d, $%d, $%d)",
			offset+1,
			offset+2,
			offset+3,
			offset+4,
			offset+5,
		)
		args = append(args, row.FactID, row.ScopeID, row.GenerationID, row.SourceRepoID, row.ReferenceKey)
	}
	if _, err := db.ExecContext(ctx, insertRelationshipReferenceCandidateKeyPrefix+values.String()+insertRelationshipReferenceCandidateKeySuffix, args...); err != nil {
		return fmt.Errorf("insert relationship reference candidate key batch (%d rows): %w", len(rows), err)
	}
	return nil
}

func acceptedFactIDs(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if strings.TrimSpace(envelope.FactID) == "" {
			continue
		}
		if _, ok := seen[envelope.FactID]; ok {
			continue
		}
		seen[envelope.FactID] = struct{}{}
		ids = append(ids, envelope.FactID)
	}
	return ids
}

const deleteRelationshipReferenceCandidateKeysSQL = `
DELETE FROM relationship_reference_candidate_keys
WHERE fact_id = ANY($1::text[])
`

const insertRelationshipReferenceCandidateKeyPrefix = `
INSERT INTO relationship_reference_candidate_keys (
    fact_id, scope_id, generation_id, source_repo_id, reference_key
) VALUES `

const insertRelationshipReferenceCandidateKeySuffix = `
ON CONFLICT (fact_id) DO UPDATE SET
    scope_id = EXCLUDED.scope_id,
    generation_id = EXCLUDED.generation_id,
    source_repo_id = EXCLUDED.source_repo_id,
    reference_key = EXCLUDED.reference_key
`
