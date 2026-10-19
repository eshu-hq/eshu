// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Closed retirement states for repository_retirements.state (#7766). The
// commit gate refuses ingest for an open row (readmitted_at IS NULL) in any
// state except complete; a complete row is tombstone history.
const (
	// RetirementStatePending marks a requested retirement the fence has not run yet.
	RetirementStatePending = "pending"
	// RetirementStateRunning marks a retirement the runner is purging.
	RetirementStateRunning = "running"
	// RetirementStateBlocked marks a retirement waiting on a live lease or lock.
	RetirementStateBlocked = "blocked"
	// RetirementStateRepairingGraph marks a retirement re-driving graph retraction.
	RetirementStateRepairingGraph = "repairing_graph"
	// RetirementStateComplete marks a finished retirement: tombstone history.
	RetirementStateComplete = "complete"
	// RetirementStateFailed marks a retirement that stopped with residue.
	RetirementStateFailed = "failed"
)

// Closed retirement phases for repository_retirements.phase (#7766).
const (
	// RetirementPhaseFenced marks a retirement whose phase-1 mark committed.
	RetirementPhaseFenced = "fenced"
	// RetirementPhaseQuiesce marks a retirement draining reducer work.
	RetirementPhaseQuiesce = "quiesce"
	// RetirementPhaseBarrier marks a retirement past the intent barrier.
	RetirementPhaseBarrier = "barrier"
	// RetirementPhaseGraphRetract marks a retirement retracting graph edges.
	RetirementPhaseGraphRetract = "graph_retract"
	// RetirementPhasePurge marks a retirement deleting Postgres rows.
	RetirementPhasePurge = "purge"
	// RetirementPhaseCensus marks a retirement running the residue census.
	RetirementPhaseCensus = "census"
	// RetirementPhaseFinalize marks a retirement deleting the scope rows.
	RetirementPhaseFinalize = "finalize"
	// RetirementPhaseDone marks a retirement that reached complete.
	RetirementPhaseDone = "done"
)

// RetirementReasonOperatorRetired is the only reason code the tombstone
// accepts: retirement is always an explicit operator act, never an automatic
// policy outcome (#7766).
const RetirementReasonOperatorRetired = "operator_retired"

// ErrRetirementAlreadyOpen reports a second open retirement for one
// repository: the caller must close or readmit the open row first.
var ErrRetirementAlreadyOpen = errors.New("repository already has an open retirement")

// RetirementRequest carries the operator inputs that open one tombstone row.
type RetirementRequest struct {
	// RetirementID is the tombstone key ('rr_' prefix).
	RetirementID string
	// RepoID is the git repository scope identity (ingestion_scopes.partition_key).
	RepoID string
	// ScopeID is the default-branch scope being retired.
	ScopeID string
	// ReasonCode is always RetirementReasonOperatorRetired.
	ReasonCode string
	// ReasonHash is the safe hash of the operator's free-text reason.
	ReasonHash string
	// ActorClass names the authenticated actor class (operator, break-glass).
	ActorClass string
	// ActorIDHash is the safe hash of the actor identity, empty when anonymous.
	ActorIDHash string
	// IdempotencyKeyHash is the safe hash of the operator's idempotency key.
	IdempotencyKeyHash string
}

// RetirementRecord is the tombstone row the gate, runner, and read surfaces share.
type RetirementRecord struct {
	// RetirementID is the tombstone key.
	RetirementID string
	// RepoID is the retired repository scope identity.
	RepoID string
	// ScopeID is the retired default-branch scope.
	ScopeID string
	// State is one of the RetirementState* values.
	State string
	// Phase is one of the RetirementPhase* values.
	Phase string
}

// RetirementPreview carries the dry-run counts the tombstone's WARN log and
// rows_deleted ledger must later account for (#7766).
type RetirementPreview struct {
	// Generations counts scope_generations rows of the scopes.
	Generations int64
	// FactWorkItems counts fact_work_items rows of the scopes.
	FactWorkItems int64
	// ContentFiles counts content_files rows of the repository.
	ContentFiles int64
	// SharedIntents counts shared_projection_intents rows of the repository or scopes.
	SharedIntents int64
}

// RetirementStore owns the repository_retirements tombstone rows. It writes
// no other table and reads only the four preview tables, so the fence, the
// runner, and the admin surface share one query contract.
type RetirementStore struct {
	database db.ExecQueryer
}

// NewRetirementStore binds the tombstone queries to the database.
func NewRetirementStore(database db.ExecQueryer) RetirementStore {
	return RetirementStore{database: database}
}

// retirementRecordColumns is the scan order InsertPending and GetOpen share.
const retirementRecordColumns = "retirement_id, repo_id, scope_id, state, phase"

// InsertPending opens a pending/fenced tombstone row for the request. A
// repeated call with the same idempotency key returns the existing open row
// without opening a second one. A different key while a row is open fails
// with ErrRetirementAlreadyOpen.
func (s RetirementStore) InsertPending(ctx context.Context, req RetirementRequest) (RetirementRecord, error) {
	if strings.TrimSpace(req.RetirementID) == "" {
		return RetirementRecord{}, fmt.Errorf("retirement insert: empty retirement id")
	}
	if strings.TrimSpace(req.RepoID) == "" {
		return RetirementRecord{}, fmt.Errorf("retirement insert: empty repo id")
	}
	if strings.TrimSpace(req.ScopeID) == "" {
		return RetirementRecord{}, fmt.Errorf("retirement insert: empty scope id")
	}
	if strings.TrimSpace(req.IdempotencyKeyHash) == "" {
		return RetirementRecord{}, fmt.Errorf("retirement insert: empty idempotency key")
	}
	if req.ReasonCode != RetirementReasonOperatorRetired {
		return RetirementRecord{}, fmt.Errorf("retirement insert: reason %q is not operator-driven", req.ReasonCode)
	}
	const insert = `INSERT INTO repository_retirements
		(retirement_id, repo_id, scope_id, state, phase, reason_code, reason_hash,
		 actor_class, actor_id_hash, idempotency_key_hash, scope_id_hash,
		 generation_ids_hash, requested_at, updated_at)
		VALUES ($1, $2, $3, 'pending', 'fenced', $4, $5, $6, $7, $8, '', '', NOW(), NOW())
		ON CONFLICT (repo_id, idempotency_key_hash) WHERE readmitted_at IS NULL
		DO UPDATE SET updated_at = NOW()
		RETURNING ` + retirementRecordColumns
	rows, err := s.database.QueryContext(ctx, insert,
		req.RetirementID, req.RepoID, req.ScopeID, req.ReasonCode,
		req.ReasonHash, req.ActorClass, req.ActorIDHash, req.IdempotencyKeyHash)
	if err != nil {
		if strings.Contains(err.Error(), "repository_retirements_open_repo_idx") {
			return RetirementRecord{}, fmt.Errorf("retirement insert %s: %w", req.RepoID, ErrRetirementAlreadyOpen)
		}
		return RetirementRecord{}, fmt.Errorf("retirement insert: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return RetirementRecord{}, fmt.Errorf("retirement insert: %w", err)
		}
		return RetirementRecord{}, fmt.Errorf("retirement insert: no row returned")
	}
	var rec RetirementRecord
	if err := rows.Scan(&rec.RetirementID, &rec.RepoID, &rec.ScopeID, &rec.State, &rec.Phase); err != nil {
		return RetirementRecord{}, fmt.Errorf("retirement insert scan: %w", err)
	}
	return rec, nil
}

// GetOpen returns the repository's open tombstone row, or nil when none is
// open. The predicate matches the partial open-row index exactly.
func (s RetirementStore) GetOpen(ctx context.Context, repoID string) (*RetirementRecord, error) {
	const lookup = `SELECT ` + retirementRecordColumns + ` FROM repository_retirements
		WHERE repo_id = $1 AND readmitted_at IS NULL LIMIT 1`
	rows, err := s.database.QueryContext(ctx, lookup, repoID)
	if err != nil {
		return nil, fmt.Errorf("retirement lookup: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var rec RetirementRecord
	if err := rows.Scan(&rec.RetirementID, &rec.RepoID, &rec.ScopeID, &rec.State, &rec.Phase); err != nil {
		return nil, fmt.Errorf("retirement lookup scan: %w", err)
	}
	return &rec, nil
}

// Preview returns the dry-run counts for the repository and scopes. It
// writes nothing and is the arbiter-review input before the irreversible
// act (#7766).
func (s RetirementStore) Preview(ctx context.Context, repoID string, scopeIDs []string) (RetirementPreview, error) {
	const preview = `SELECT
		(SELECT count(*) FROM scope_generations WHERE scope_id = ANY($2)),
		(SELECT count(*) FROM fact_work_items WHERE scope_id = ANY($2)),
		(SELECT count(*) FROM content_files WHERE repo_id = $1),
		(SELECT count(*) FROM shared_projection_intents WHERE repository_id = $1 OR scope_id = ANY($2))`
	rows, err := s.database.QueryContext(ctx, preview, repoID, scopeIDs)
	if err != nil {
		return RetirementPreview{}, fmt.Errorf("retirement preview: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return RetirementPreview{}, fmt.Errorf("retirement preview: %w", err)
		}
		return RetirementPreview{}, fmt.Errorf("retirement preview: no row returned")
	}
	var counts RetirementPreview
	if err := rows.Scan(&counts.Generations, &counts.FactWorkItems, &counts.ContentFiles, &counts.SharedIntents); err != nil {
		return RetirementPreview{}, fmt.Errorf("retirement preview scan: %w", err)
	}
	return counts, nil
}
