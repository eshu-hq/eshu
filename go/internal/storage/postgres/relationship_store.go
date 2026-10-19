// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// RelationshipStore persists relationship evidence, assertions, candidates,
// and resolved relationships in PostgreSQL.
type RelationshipStore struct {
	database db.ExecQueryer
	// activationLockTimeout bounds the #7597 activation wake's wait on an
	// in-flight consumer row; zero uses defaultActivationWakeLockTimeout.
	// Tests shorten it.
	activationLockTimeout time.Duration
	// instruments records eshu_dp_reducer_own_gate_wake_total for the
	// activation wake (#7597). Nil is a no-op.
	instruments *telemetry.Instruments
}

// WithInstruments attaches the telemetry instruments the activation wake
// records on (#7597) and returns the store. Nil keeps it uninstrumented.
func (s *RelationshipStore) WithInstruments(instruments *telemetry.Instruments) *RelationshipStore {
	s.instruments = instruments
	return s
}

// NewRelationshipStore constructs a Postgres-backed relationship store.
func NewRelationshipStore(database db.ExecQueryer) *RelationshipStore {
	return &RelationshipStore{database: database}
}

// EnsureSchema applies the relationship DDL.
func (s *RelationshipStore) EnsureSchema(ctx context.Context) error {
	_, err := s.database.ExecContext(ctx, relationshipSchemaSQL)
	return err
}

// UpsertAssertions persists one or more relationship assertions.
func (s *RelationshipStore) UpsertAssertions(
	ctx context.Context,
	assertions []relationships.Assertion,
) error {
	if len(assertions) == 0 {
		return nil
	}

	now := time.Now().UTC()
	for _, a := range assertions {
		assertionID := relationshipDigest(
			"assertion",
			string(a.RelationshipType), a.SourceRepoID, a.TargetRepoID,
		)
		srcEntityID := coalesceNullable(a.SourceEntityID, a.SourceRepoID)
		tgtEntityID := coalesceNullable(a.TargetEntityID, a.TargetRepoID)
		if _, err := s.database.ExecContext(
			ctx, upsertAssertionSQL,
			assertionID,
			a.SourceRepoID,
			a.TargetRepoID,
			srcEntityID,
			tgtEntityID,
			string(a.RelationshipType),
			a.Decision,
			a.Reason,
			a.Actor,
			now,
			now,
		); err != nil {
			return fmt.Errorf("upsert assertion: %w", err)
		}
	}
	return nil
}

// ListAssertions returns stored assertions, optionally filtered by type.
func (s *RelationshipStore) ListAssertions(
	ctx context.Context,
	relationshipType *relationships.RelationshipType,
) ([]relationships.Assertion, error) {
	var sqlRows db.Rows
	var err error

	if relationshipType == nil {
		sqlRows, err = s.database.QueryContext(ctx, listAssertionsSQL)
	} else {
		sqlRows, err = s.database.QueryContext(ctx, listAssertionsByTypeSQL, string(*relationshipType))
	}
	if err != nil {
		return nil, fmt.Errorf("list assertions: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var result []relationships.Assertion
	for sqlRows.Next() {
		var a relationships.Assertion
		var relType string
		if err := sqlRows.Scan(
			&a.SourceRepoID,
			&a.TargetRepoID,
			&a.SourceEntityID,
			&a.TargetEntityID,
			&relType,
			&a.Decision,
			&a.Reason,
			&a.Actor,
		); err != nil {
			return nil, fmt.Errorf("scan assertion: %w", err)
		}
		a.RelationshipType = relationships.RelationshipType(relType)
		result = append(result, a)
	}
	return result, sqlRows.Err()
}

// CreateGeneration creates a new pending generation for the given scope.
func (s *RelationshipStore) CreateGeneration(
	ctx context.Context,
	scopeID string,
	runID string,
) (string, error) {
	now := time.Now().UTC()
	genID := relationshipDigest("generation", scopeID, runID, fmt.Sprintf("%d", now.UnixNano()))
	if _, err := s.database.ExecContext(
		ctx, createGenerationSQL,
		genID, scopeID, payloadstore.EmptyToNil(runID), now,
	); err != nil {
		return "", fmt.Errorf("create generation: %w", err)
	}
	return genID, nil
}

// CommitGeneration promotes a pending generation to active status.
func (s *RelationshipStore) CommitGeneration(
	ctx context.Context,
	generationID string,
	scopeID string,
) error {
	now := time.Now().UTC()
	if _, err := s.activateWithConsumerWake(ctx, activateGenerationSQL,
		[]any{now, generationID, scopeID}, scopeID, generationID, now); err != nil {
		return fmt.Errorf("commit generation: %w", err)
	}
	return nil
}

// ActivateResolutionGeneration creates and activates a relationship generation
// in a single idempotent operation. This makes resolved relationships visible
// to downstream consumers (e.g. workload materialization) that query by scope.
func (s *RelationshipStore) ActivateResolutionGeneration(
	ctx context.Context,
	generationID string,
	scopeID string,
) error {
	now := time.Now().UTC()
	if _, err := s.activateWithConsumerWake(ctx, activateResolutionGenerationSQL,
		[]any{generationID, scopeID, now, now}, scopeID, generationID, now); err != nil {
		return fmt.Errorf("activate resolution generation: %w", err)
	}
	return nil
}

// scopeRepositoryIDsSQL lists the git repository IDs whose facts belong to a
// scope generation. The cross-repo resolver uses it to attribute each resolved
// edge to the scope that owns its source repository (single-writer per edge).
// It mirrors the repository-fact predicate of the reducer-graph-drain
// canonical-code probe: live (non-tombstone) git repository facts with a
// non-empty repo_id. Served by fact_records_scope_generation_idx.
const scopeRepositoryIDsSQL = `
SELECT DISTINCT fact.payload ->> 'repo_id'
FROM fact_records AS fact
WHERE fact.scope_id = $1
  AND fact.generation_id = $2
  AND fact.fact_kind = 'repository'
  AND fact.source_system = 'git'
  AND fact.is_tombstone = FALSE
  AND NULLIF(fact.payload ->> 'repo_id', '') IS NOT NULL`

// ListScopeRepositoryIDs implements crossrepo.ScopeRepositoryReader.
func (s *RelationshipStore) ListScopeRepositoryIDs(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]string, error) {
	rows, err := s.database.QueryContext(ctx, scopeRepositoryIDsSQL, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list scope repository IDs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var repoIDs []string
	for rows.Next() {
		var repoID string
		if err := rows.Scan(&repoID); err != nil {
			return nil, fmt.Errorf("scan scope repository ID: %w", err)
		}
		repoIDs = append(repoIDs, repoID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scope repository IDs: %w", err)
	}
	return repoIDs, nil
}

// IsGenerationActive reports whether the relationship generation is currently
// active (published). It is a primary-key lookup on relationship_generations and
// backs the repo-dependency graph-projection authority gate: graph edges for a
// generation must not be projected until the generation is activated, so the
// graph cannot run ahead of the Postgres relationship read models that filter on
// status = 'active'.
func (s *RelationshipStore) IsGenerationActive(
	ctx context.Context,
	generationID string,
) (bool, error) {
	rows, err := s.database.QueryContext(ctx, relationshipGenerationActiveSQL, generationID)
	if err != nil {
		return false, fmt.Errorf("query relationship generation active: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, rows.Err()
	}
	return true, rows.Err()
}

// ListEvidenceFacts returns stored evidence facts for a generation.
func (s *RelationshipStore) ListEvidenceFacts(
	ctx context.Context,
	generationID string,
) ([]relationships.EvidenceFact, error) {
	sqlRows, err := s.database.QueryContext(ctx, listEvidenceFactsByGenerationSQL, generationID)
	if err != nil {
		return nil, fmt.Errorf("list evidence facts: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var result []relationships.EvidenceFact
	for sqlRows.Next() {
		var f relationships.EvidenceFact
		var evidenceKind, relType string
		var detailsBytes []byte
		if err := sqlRows.Scan(
			&evidenceKind,
			&relType,
			&f.SourceRepoID,
			&f.TargetRepoID,
			&f.SourceEntityID,
			&f.TargetEntityID,
			&f.Confidence,
			&f.Rationale,
			&detailsBytes,
		); err != nil {
			return nil, fmt.Errorf("scan evidence fact: %w", err)
		}
		f.EvidenceKind = relationships.EvidenceKind(evidenceKind)
		f.RelationshipType = relationships.RelationshipType(relType)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &f.Details); err != nil {
				return nil, fmt.Errorf("unmarshal evidence details: %w", err)
			}
		}
		result = append(result, f)
	}
	return result, sqlRows.Err()
}

// UpsertCandidates persists relationship candidates for a generation.
func (s *RelationshipStore) UpsertCandidates(
	ctx context.Context,
	generationID string,
	candidates []relationships.Candidate,
) error {
	if len(candidates) == 0 {
		return nil
	}

	for i, c := range candidates {
		candidateID := relationshipDigest(
			"candidate", generationID,
			c.SourceEntityID, c.TargetEntityID,
			string(c.RelationshipType), fmt.Sprintf("%d", i),
		)
		detailsJSON, err := json.Marshal(c.Details)
		if err != nil {
			return fmt.Errorf("marshal candidate details: %w", err)
		}
		if _, err := s.database.ExecContext(
			ctx, insertCandidateSQL,
			candidateID,
			generationID,
			payloadstore.EmptyToNil(c.SourceRepoID),
			payloadstore.EmptyToNil(c.TargetRepoID),
			payloadstore.EmptyToNil(c.SourceEntityID),
			payloadstore.EmptyToNil(c.TargetEntityID),
			string(c.RelationshipType),
			c.Confidence,
			c.EvidenceCount,
			c.Rationale,
			detailsJSON,
		); err != nil {
			return fmt.Errorf("insert candidate: %w", err)
		}
	}
	return nil
}

// UpsertResolved persists resolved relationships for a generation.
//
// It reports what the ON CONFLICT DO NOTHING insert actually wrote: Inserted
// counts rows with RowsAffected > 0 (a re-run over stored identities inserts
// nothing), and RepoIDs holds the distinct non-empty source/target repository
// IDs of exactly those inserted rows. The completion-driven
// deployable_unit_correlation wake (#8294) keys off that set: a re-run that
// appends under an already-active generation reports the appended rows'
// repos, and a pure re-run reports zero and reopens nothing.
func (s *RelationshipStore) UpsertResolved(
	ctx context.Context,
	generationID string,
	resolved []relationships.ResolvedRelationship,
) (crossrepo.ResolvedUpsertResult, error) {
	if len(resolved) == 0 {
		return crossrepo.ResolvedUpsertResult{}, nil
	}

	var result crossrepo.ResolvedUpsertResult
	seen := make(map[string]struct{}, 2*len(resolved))
	for _, r := range resolved {
		resolvedID := relationships.ResolvedRelationshipID(generationID, r)
		detailsJSON, err := json.Marshal(r.Details)
		if err != nil {
			return crossrepo.ResolvedUpsertResult{}, fmt.Errorf("marshal resolved details: %w", err)
		}
		execResult, err := s.database.ExecContext(
			ctx, insertResolvedSQL,
			resolvedID,
			generationID,
			payloadstore.EmptyToNil(r.SourceRepoID),
			payloadstore.EmptyToNil(r.TargetRepoID),
			payloadstore.EmptyToNil(r.SourceEntityID),
			payloadstore.EmptyToNil(r.TargetEntityID),
			string(r.RelationshipType),
			r.Confidence,
			r.EvidenceCount,
			r.Rationale,
			string(r.ResolutionSource),
			detailsJSON,
		)
		if err != nil {
			return crossrepo.ResolvedUpsertResult{}, fmt.Errorf("insert resolved: %w", err)
		}
		affected, err := execResult.RowsAffected()
		if err != nil {
			return crossrepo.ResolvedUpsertResult{}, fmt.Errorf("insert resolved: rows affected: %w", err)
		}
		if affected == 0 {
			continue
		}
		result.Inserted += affected
		for _, repoID := range []string{r.SourceRepoID, r.TargetRepoID} {
			repoID = strings.TrimSpace(repoID)
			if repoID == "" {
				continue
			}
			if _, ok := seen[repoID]; ok {
				continue
			}
			seen[repoID] = struct{}{}
			result.RepoIDs = append(result.RepoIDs, repoID)
		}
	}
	return result, nil
}

// GetResolvedRelationships returns resolved relationships for the active
// generation in a scope.
func (s *RelationshipStore) GetResolvedRelationships(
	ctx context.Context,
	scopeID string,
) ([]relationships.ResolvedRelationship, error) {
	sqlRows, err := s.database.QueryContext(ctx, listResolvedSQL, scopeID)
	if err != nil {
		return nil, fmt.Errorf("list resolved: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var result []relationships.ResolvedRelationship
	for sqlRows.Next() {
		var r relationships.ResolvedRelationship
		var sourceRepoID sql.NullString
		var targetRepoID sql.NullString
		var sourceEntityID sql.NullString
		var targetEntityID sql.NullString
		var relType, resSrc string
		var detailsBytes []byte
		if err := sqlRows.Scan(
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
			return nil, fmt.Errorf("scan resolved: %w", err)
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, fmt.Errorf("unmarshal resolved details: %w", err)
			}
		}
		result = append(result, r)
	}
	return result, sqlRows.Err()
}

// GetResolvedRelationshipsForGeneration returns resolved relationships for one
// exact relationship generation inside a scope.
func (s *RelationshipStore) GetResolvedRelationshipsForGeneration(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]relationships.ResolvedRelationship, error) {
	sqlRows, err := s.database.QueryContext(ctx, listResolvedByGenerationSQL, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list resolved by generation: %w", err)
	}
	defer func() { _ = sqlRows.Close() }()

	var result []relationships.ResolvedRelationship
	for sqlRows.Next() {
		var r relationships.ResolvedRelationship
		var sourceRepoID sql.NullString
		var targetRepoID sql.NullString
		var sourceEntityID sql.NullString
		var targetEntityID sql.NullString
		var relType, resSrc string
		var detailsBytes []byte
		if err := sqlRows.Scan(
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
			return nil, fmt.Errorf("scan resolved by generation: %w", err)
		}
		r.SourceRepoID = nullableString(sourceRepoID)
		r.TargetRepoID = nullableString(targetRepoID)
		r.SourceEntityID = nullableString(sourceEntityID)
		r.TargetEntityID = nullableString(targetEntityID)
		r.RelationshipType = relationships.RelationshipType(relType)
		r.ResolutionSource = relationships.ResolutionSource(resSrc)
		if len(detailsBytes) > 0 {
			if err := json.Unmarshal(detailsBytes, &r.Details); err != nil {
				return nil, fmt.Errorf("unmarshal resolved by generation details: %w", err)
			}
		}
		result = append(result, r)
	}
	return result, sqlRows.Err()
}

func nullableString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
