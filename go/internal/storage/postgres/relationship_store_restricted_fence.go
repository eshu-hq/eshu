// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Per-item read-set waits (#10877): the restricted corpus fence.
//
// The narrowed path replaces the corpus-wide fence with a per-item wait keyed
// on the producing scopes of exactly the repositories the item reads. The
// store computes the blocking scope set inside the same statement snapshot
// as the restricted verdict and the rows it admits, so a holder that
// activates mid-read cannot slip between a separate verdict and the rows it
// admits (#6740 invariant preserved).
//
// A holder is blocking unless it is a proven non-producer for the read set:
// it has a relationship generation row (a holder with no generation row yet
// can never be excluded) AND no active resolved row touching the set in any
// generation AND no live deployment_mapping work that could still produce
// one. Every exclusion needs all three evidences at once; anything less
// waits, fail-safe.
//
// The statement takes the repository set once ($1 text[]) and reads it in
// every CTE: the blocking computation, the restricted fence, and the
// by-repos match (the = ANY form selects the same rows as the shipped IN
// list; the fingerprint differential test proves it). The shapes below keep
// the derivation markers of deriveFingerprintedFusedRead byte-identical so
// the fingerprinted restricted read is derived, never copied.

// restrictedBlockingScopesCTE lists the blocking scopes for the statement's
// repository set: incomplete holders minus proven non-producers, ordered, so
// the verdict-time set renders deterministically.
const restrictedBlockingScopesCTE = `
blocking AS MATERIALIZED (
  SELECT s.scope_id
  FROM ingestion_scopes AS s
  WHERE` + incompleteScopeRelationshipGenerationsPredicate + `
    AND NOT (
      EXISTS (
        SELECT 1
        FROM relationship_generations AS rg
        WHERE rg.scope = s.scope_id
      )
      AND NOT EXISTS (
        SELECT 1
        FROM resolved_relationships AS r
        JOIN relationship_generations AS g
          ON g.generation_id = r.generation_id
        WHERE g.scope = s.scope_id
          AND (
            r.source_repo_id = ANY ($1::text[])
            OR r.target_repo_id = ANY ($1::text[])
          )
      )
      AND NOT EXISTS (
        SELECT 1
        FROM fact_work_items AS w
        WHERE w.scope_id = s.scope_id
          AND w.stage = 'reducer'
          AND w.domain = 'deployment_mapping'
          AND w.status IN ('pending', 'claimed', 'running', 'retrying')
      )
    )
)`

// listResolvedByReposWithRestrictedFenceSQL fuses the restricted corpus fence
// and the by-repos resolved read into ONE statement, mirroring
// listResolvedByReposWithCorpusFenceSQL: same row shape and order, plus the
// verdict-time blocking set every row carries. When complete is false the
// rows are NULL filler and must not be consumed.
const listResolvedByReposWithRestrictedFenceSQL = `
WITH` + restrictedBlockingScopesCTE + `,
fence AS MATERIALIZED (
  SELECT NOT EXISTS (
    SELECT 1
    FROM ingestion_scopes AS s
    WHERE` + incompleteScopeRelationshipGenerationsPredicate + `
      AND s.scope_id IN (SELECT scope_id FROM blocking)
  ) AS complete,
  (SELECT COALESCE(array_agg(scope_id ORDER BY scope_id), '{}') FROM blocking) AS blocking
)
SELECT fence.complete, fence.blocking,
       r.source_repo_id, r.target_repo_id,
       r.source_key, r.target_key,
       r.relationship_type,
       COALESCE(r.confidence, 0), COALESCE(r.evidence_count, 0),
       COALESCE(r.rationale, ''), COALESCE(r.resolution_source, ''), r.details
FROM fence
LEFT JOIN (
  SELECT r.source_repo_id, r.target_repo_id,
         COALESCE(r.source_entity_id, r.source_repo_id) AS source_key,
         COALESCE(r.target_entity_id, r.target_repo_id) AS target_key,
         r.relationship_type, r.confidence, r.evidence_count,
         r.rationale, r.resolution_source, r.details
  FROM resolved_relationships AS r
  JOIN relationship_generations AS g
    ON g.generation_id = r.generation_id
  WHERE g.status = 'active'
    AND (
      r.source_repo_id = ANY ($1::text[])
      OR r.target_repo_id = ANY ($1::text[])
    )
) AS r ON fence.complete
ORDER BY r.source_key, r.target_key, r.relationship_type
`

// blockingScopeIDsSQL lists the blocking scope IDs for one repository set,
// the same verdict the restricted fence evaluates, for the deferral's wait
// key and for diagnostics. $1 is the text[] repository set.
const blockingScopeIDsSQL = `WITH` + restrictedBlockingScopesCTE + `
SELECT scope_id FROM blocking ORDER BY scope_id`

// BlockingRelationshipScopes returns the verdict-time blocking scope set for
// a repository set: incomplete holders minus proven non-producers, ordered.
// Empty for an empty set: a run that reads no by-repos input waits on
// nothing.
func (s *RelationshipStore) BlockingRelationshipScopes(
	ctx context.Context,
	repoIDs []string,
) ([]string, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil, nil
	}
	rows, err := s.database.QueryContext(ctx, blockingScopeIDsSQL, array.StringArray(repoIDs))
	if err != nil {
		return nil, fmt.Errorf("list blocking relationship scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			return nil, fmt.Errorf("scan blocking relationship scope: %w", err)
		}
		if strings.TrimSpace(scopeID) == "" {
			continue
		}
		out = append(out, scopeID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate blocking relationship scopes: %w", err)
	}
	return out, nil
}

// GetResolvedRelationshipsForReposWithRestrictedFence returns the active
// resolved relationships touching the repository set together with the
// restricted corpus verdict and the verdict-time blocking scope set, all from
// one statement snapshot. complete is false while any blocking scope's
// current relationship generation is retired-or-pending; the rows are then
// nil and must not be consumed. A query error fails safe, never a verdict.
func (s *RelationshipStore) GetResolvedRelationshipsForReposWithRestrictedFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	if len(repoIDs) == 0 {
		return nil, nil, true, nil
	}
	sqlRows, err := s.database.QueryContext(ctx,
		listResolvedByReposWithRestrictedFenceSQL, array.StringArray(repoIDs))
	if err != nil {
		return nil, nil, false, fmt.Errorf("list resolved by repos with restricted fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	return scanRestrictedFencedResolvedRows(sqlRows, nil)
}

// listResolvedByReposWithRestrictedFenceAndFingerprintSQL is the restricted
// fused read with the #8283 input fingerprint, derived by the same marker
// rewrite the global fingerprinted read uses, so the verdict, the blocking
// set, the rows, and the fingerprint of exactly those rows come from one
// statement snapshot and the rows are read once.
var listResolvedByReposWithRestrictedFenceAndFingerprintSQL = deriveFingerprintedRestrictedFusedRead(
	listResolvedByReposWithRestrictedFenceSQL,
)

// deriveFingerprintedRestrictedFusedRead derives the fingerprinted restricted
// read with the scanner's column order (complete, blocking, fingerprint,
// rows): the shared derivation emits the fingerprint before the carried
// blocking column, so the two are swapped here. It panics when the emitted
// shape is not exactly as derived, so a rewrite fails at initialization.
func deriveFingerprintedRestrictedFusedRead(fused string) string {
	derived := deriveFingerprintedFusedRead(fused)
	const want = "SELECT fence.complete, input_fingerprint.value, fence.blocking,"
	const ordered = "SELECT fence.complete, fence.blocking, input_fingerprint.value,"
	if count := strings.Count(derived, want); count != 1 {
		panic(fmt.Sprintf("derive fingerprinted restricted fused read: fingerprint-before-blocking occurs %d times, want 1", count))
	}
	return strings.Replace(derived, want, ordered, 1)
}

// GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence is the
// restricted read plus the input fingerprint of the rows it returned,
// computed in the same statement. The handler records the fingerprint on
// success and the reopen gates compare against it (#10877 parity with #8283).
func (s *RelationshipStore) GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(
	ctx context.Context,
	repoIDs []string,
) ([]relationships.ResolvedRelationship, []string, bool, reducercontract.ResolvedReadFingerprint, error) {
	repoIDs = uniqueNonEmptyStrings(repoIDs)
	fingerprint := reducercontract.ResolvedReadFingerprint{RepoIDs: repoIDs}
	if len(repoIDs) == 0 {
		fingerprint.Value = reducercontract.EmptyResolvedReadFingerprint
		return nil, nil, true, fingerprint, nil
	}
	sqlRows, err := s.database.QueryContext(ctx,
		listResolvedByReposWithRestrictedFenceAndFingerprintSQL, array.StringArray(repoIDs))
	if err != nil {
		return nil, nil, false, fingerprint, fmt.Errorf("list fingerprinted resolved by repos with restricted fence: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	resolved, blocking, complete, err := scanRestrictedFencedResolvedRows(sqlRows, &fingerprint.Value)
	if err != nil {
		return nil, nil, false, fingerprint, err
	}
	fingerprint.RepoIDs = repoIDs
	return resolved, blocking, complete, fingerprint, nil
}

// scanRestrictedFencedResolvedRows reads the restricted fused result: every
// row carries the same verdict and verdict-time blocking set, and -- on the
// fingerprinted statement -- the input fingerprint right after them. Rows
// with a NULL relationship_type are the LEFT JOIN filler for an incomplete
// corpus or an empty match. No row at all is impossible from the LEFT JOIN
// shape and is reported as an error rather than read as a verdict.
func scanRestrictedFencedResolvedRows(
	rows db.Rows, fingerprint *string,
) ([]relationships.ResolvedRelationship, []string, bool, error) {
	var result []relationships.ResolvedRelationship
	var blocking []string
	sawRow := false
	complete := false
	for rows.Next() {
		var rowComplete bool
		var rowBlocking array.StringArray
		var sourceRepoID, targetRepoID, sourceEntityID, targetEntityID, relType sql.NullString
		var r relationships.ResolvedRelationship
		var resSrc string
		var detailsBytes []byte
		dest := []any{&rowComplete, &rowBlocking}
		if fingerprint != nil {
			dest = append(dest, fingerprint)
		}
		if err := rows.Scan(append(dest,
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
		)...); err != nil {
			return nil, nil, false, fmt.Errorf("scan resolved by repos with restricted fence: %w", err)
		}
		sawRow = true
		complete = rowComplete
		blocking = []string(rowBlocking)
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
				return nil, nil, false, fmt.Errorf("unmarshal resolved by repos with restricted fence details: %w", err)
			}
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, fmt.Errorf("iterate resolved by repos with restricted fence: %w", err)
	}
	if !sawRow {
		return nil, nil, false, errCorpusFencedResolvedNoRows
	}
	if !complete {
		return nil, blocking, false, nil
	}
	return result, blocking, true, nil
}
