package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// RelationshipStore persists relationship evidence, assertions, candidates,
// and resolved relationships in PostgreSQL.
type RelationshipStore struct {
	db ExecQueryer
}

// NewRelationshipStore constructs a Postgres-backed relationship store.
func NewRelationshipStore(db ExecQueryer) *RelationshipStore {
	return &RelationshipStore{db: db}
}

// EnsureSchema applies the relationship DDL.
func (s *RelationshipStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, relationshipSchemaSQL)
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
		assertionID := relationshipDigest("assertion",
			string(a.RelationshipType), a.SourceRepoID, a.TargetRepoID,
		)
		srcEntityID := coalesceNullable(a.SourceEntityID, a.SourceRepoID)
		tgtEntityID := coalesceNullable(a.TargetEntityID, a.TargetRepoID)
		if _, err := s.db.ExecContext(ctx, upsertAssertionSQL,
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
	var sqlRows Rows
	var err error

	if relationshipType == nil {
		sqlRows, err = s.db.QueryContext(ctx, listAssertionsSQL)
	} else {
		sqlRows, err = s.db.QueryContext(ctx, listAssertionsByTypeSQL, string(*relationshipType))
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
	if _, err := s.db.ExecContext(ctx, createGenerationSQL,
		genID, scopeID, emptyToNil(runID), now,
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
	_, err := s.db.ExecContext(ctx, activateGenerationSQL, now, generationID, scopeID)
	if err != nil {
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
	_, err := s.db.ExecContext(ctx, activateResolutionGenerationSQL,
		generationID, scopeID, now, now,
	)
	if err != nil {
		return fmt.Errorf("activate resolution generation: %w", err)
	}
	return nil
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
	rows, err := s.db.QueryContext(ctx, relationshipGenerationActiveSQL, generationID)
	if err != nil {
		return false, fmt.Errorf("query relationship generation active: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return false, rows.Err()
	}
	return true, rows.Err()
}

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
	if len(facts) == 0 {
		return nil
	}
	facts = relationships.DedupeEvidenceFacts(facts)
	if len(facts) == 0 {
		return nil
	}

	now := time.Now().UTC()
	for start := 0; start < len(facts); start += evidenceInsertBatchRows {
		end := start + evidenceInsertBatchRows
		if end > len(facts) {
			end = len(facts)
		}
		if err := s.insertEvidenceFactBatch(ctx, generationID, facts[start:end], now); err != nil {
			return err
		}
	}
	return nil
}

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
		args = append(args,
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

// ListEvidenceFacts returns stored evidence facts for a generation.
func (s *RelationshipStore) ListEvidenceFacts(
	ctx context.Context,
	generationID string,
) ([]relationships.EvidenceFact, error) {
	sqlRows, err := s.db.QueryContext(ctx, listEvidenceFactsByGenerationSQL, generationID)
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
		candidateID := relationshipDigest("candidate", generationID,
			c.SourceEntityID, c.TargetEntityID,
			string(c.RelationshipType), fmt.Sprintf("%d", i),
		)
		detailsJSON, err := json.Marshal(c.Details)
		if err != nil {
			return fmt.Errorf("marshal candidate details: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, insertCandidateSQL,
			candidateID,
			generationID,
			emptyToNil(c.SourceRepoID),
			emptyToNil(c.TargetRepoID),
			emptyToNil(c.SourceEntityID),
			emptyToNil(c.TargetEntityID),
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
func (s *RelationshipStore) UpsertResolved(
	ctx context.Context,
	generationID string,
	resolved []relationships.ResolvedRelationship,
) error {
	if len(resolved) == 0 {
		return nil
	}

	for i, r := range resolved {
		resolvedID := relationships.ResolvedRelationshipID(generationID, r, i)
		detailsJSON, err := json.Marshal(r.Details)
		if err != nil {
			return fmt.Errorf("marshal resolved details: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, insertResolvedSQL,
			resolvedID,
			generationID,
			emptyToNil(r.SourceRepoID),
			emptyToNil(r.TargetRepoID),
			emptyToNil(r.SourceEntityID),
			emptyToNil(r.TargetEntityID),
			string(r.RelationshipType),
			r.Confidence,
			r.EvidenceCount,
			r.Rationale,
			string(r.ResolutionSource),
			detailsJSON,
		); err != nil {
			return fmt.Errorf("insert resolved: %w", err)
		}
	}
	return nil
}

// GetResolvedRelationships returns resolved relationships for the active
// generation in a scope.
func (s *RelationshipStore) GetResolvedRelationships(
	ctx context.Context,
	scopeID string,
) ([]relationships.ResolvedRelationship, error) {
	sqlRows, err := s.db.QueryContext(ctx, listResolvedSQL, scopeID)
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
	sqlRows, err := s.db.QueryContext(ctx, listResolvedByGenerationSQL, scopeID, generationID)
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
