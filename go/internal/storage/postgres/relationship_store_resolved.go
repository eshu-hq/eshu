// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// GetResolvedRelationshipsForRepos returns active resolved relationships that
// touch any candidate repository as either source or target.
func (s *RelationshipStore) GetResolvedRelationshipsForRepos(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	placeholderList, args := repoIDPlaceholders(repoIDs)
	sqlRows, err := s.database.QueryContext(ctx, fmt.Sprintf(listResolvedByReposSQL, placeholderList, placeholderList), args...)
	if err != nil {
		return nil, fmt.Errorf("list resolved by repos: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanResolvedRelationshipRows(sqlRows, "by repos")
}

// errCorpusFencedResolvedNoRows reports a fused fence-and-read result with no
// row at all. The LEFT JOIN from the single-row fence CTE always yields one,
// so this means the backend violated the query shape; it fails the read
// rather than inventing a verdict.
var errCorpusFencedResolvedNoRows = errors.New(
	"list resolved by repos with corpus fence: no rows returned",
)

// GetResolvedRelationshipsForReposWithCorpusFence returns the active resolved
// relationships touching any candidate repository together with the
// corpus-completeness fence verdict, both from ONE statement snapshot
// (#6740), so a foreign scope that retires and re-activates its generation
// mid-pass can never pair a passing verdict with a partial row set. When
// complete is false the rows are nil and must not be consumed. An empty
// repository set still evaluates the fence (there is no read to couple it
// to). A query error fails safe: the caller receives the error, never a
// verdict.
func (s *RelationshipStore) GetResolvedRelationshipsForReposWithCorpusFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, bool, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		complete, err := s.AreActiveScopeRelationshipGenerationsComplete(ctx)
		if err != nil {
			return nil, false, err
		}
		return nil, complete, nil
	}

	placeholderList, args := repoIDPlaceholders(repoIDs)
	sqlRows, err := s.database.QueryContext(
		ctx, fmt.Sprintf(listResolvedByReposWithCorpusFenceSQL, placeholderList, placeholderList), args...,
	)
	if err != nil {
		return nil, false, fmt.Errorf("list resolved by repos with corpus fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanCorpusFencedResolvedRows(sqlRows)
}

// scanCorpusFencedResolvedRows reads the fused fence-and-read result: every
// row carries the same fence verdict (one materialized CTE row), and rows with
// a NULL relationship_type are the LEFT JOIN filler for an incomplete corpus
// or an empty match. No row at all is impossible from the LEFT JOIN shape and
// is reported as an error rather than read as a verdict.
func scanCorpusFencedResolvedRows(rows db.Rows) ([]relationships.ResolvedRelationship, bool, error) {
	var result []relationships.ResolvedRelationship
	sawRow := false
	complete := false
	for rows.Next() {
		var rowComplete bool
		var sourceRepoID, targetRepoID, sourceEntityID, targetEntityID, relType sql.NullString
		var r relationships.ResolvedRelationship
		var resSrc string
		var detailsBytes []byte
		if err := rows.Scan(
			&rowComplete,
			&sourceRepoID,
			&targetRepoID,
			&sourceEntityID,
			&targetEntityID,
			&relType,
			&r.Confidence,
			&r.EvidenceCount,
			&r.Rationale,
			&resSrc,
			&detailsBytes,
		); err != nil {
			return nil, false, fmt.Errorf("scan resolved by repos with corpus fence: %w", err)
		}
		sawRow = true
		complete = rowComplete
		if !rowComplete || !relType.Valid {
			continue
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType.String)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, false, fmt.Errorf("unmarshal resolved by repos with corpus fence details: %w", err)
			}
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate resolved by repos with corpus fence: %w", err)
	}
	if !sawRow {
		return nil, false, errCorpusFencedResolvedNoRows
	}
	if !complete {
		return nil, false, nil
	}
	return result, true, nil
}

// repoIDPlaceholders renders the positional placeholder list and bind
// arguments shared by the by-repos reads, which repeat the list for the
// source and target predicates.
func repoIDPlaceholders(repoIDs []string) (string, []any) {
	placeholders := make([]string, len(repoIDs))
	args := make([]any, len(repoIDs))
	for i, repoID := range repoIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = repoID
	}
	return strings.Join(placeholders, ", "), args
}

func scanResolvedRelationshipRows(rows db.Rows, label string) ([]relationships.ResolvedRelationship, error) {
	var result []relationships.ResolvedRelationship
	for rows.Next() {
		var r relationships.ResolvedRelationship
		var sourceRepoID sql.NullString
		var targetRepoID sql.NullString
		var sourceEntityID sql.NullString
		var targetEntityID sql.NullString
		var relType, resSrc string
		var detailsBytes []byte
		if err := rows.Scan(
			&sourceRepoID,
			&targetRepoID,
			&sourceEntityID,
			&targetEntityID,
			&relType,
			&r.Confidence,
			&r.EvidenceCount,
			&r.Rationale,
			&resSrc,
			&detailsBytes,
		); err != nil {
			return nil, fmt.Errorf("scan resolved %s: %w", label, err)
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, fmt.Errorf("unmarshal resolved %s details: %w", label, err)
			}
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// ActivateResolutionGenerationForClaim activates a relationship generation
// only while the exact reducer claim that produced it remains live. Locking the
// queue row in the same statement serializes publication against recovery's
// deletion; matching last_attempt_at rejects an older execution after reclaim.
func (s *RelationshipStore) ActivateResolutionGenerationForClaim(
	ctx context.Context,
	generationID string,
	scopeID string,
	workItemID string,
	claimedAt time.Time,
) error {
	now := time.Now().UTC()
	result, err := s.database.ExecContext(
		ctx,
		activateResolutionGenerationForClaimSQL,
		generationID,
		scopeID,
		now,
		now,
		workItemID,
		claimedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("activate resolution generation for reducer claim: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("activate resolution generation for reducer claim: rows affected: %w", err)
	}
	if affected == 0 {
		return reducercontract.ErrExecutionClaimRejected
	}
	return nil
}
