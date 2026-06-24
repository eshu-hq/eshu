// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ServiceMaterializationRow is the single-column cursor the supersede query
// returns. It is the narrow read surface the lineage writer needs, so the writer
// can be exercised with a fake transaction in unit tests instead of a live
// *sql.Tx.
type ServiceMaterializationRow interface {
	Scan(...any) error
}

// ServiceMaterializationTx is the narrow transactional surface the lineage
// writer needs: the supersede + insert + snapshot writes commit atomically under
// one transaction. *sql.Tx satisfies it through ServiceMaterializationSQLTx.
type ServiceMaterializationTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) ServiceMaterializationRow
	Commit() error
	Rollback() error
}

// ServiceMaterializationBeginner opens a transaction for the lineage commit.
// ServiceMaterializationSQLBeginner adapts a *sql.DB into this surface.
type ServiceMaterializationBeginner interface {
	BeginServiceMaterializationTx(context.Context) (ServiceMaterializationTx, error)
}

// ServiceMaterializationSQLBeginner adapts a *sql.DB into the lineage writer's
// transaction surface so production wiring stays on database/sql while tests use
// an in-memory fake.
type ServiceMaterializationSQLBeginner struct {
	DB *sql.DB
}

// BeginServiceMaterializationTx opens a database/sql transaction wrapped in the
// narrow lineage-writer surface.
func (b ServiceMaterializationSQLBeginner) BeginServiceMaterializationTx(
	ctx context.Context,
) (ServiceMaterializationTx, error) {
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return ServiceMaterializationSQLTx{Tx: tx}, nil
}

// ServiceMaterializationSQLTx adapts a *sql.Tx into the lineage-writer
// transaction surface.
type ServiceMaterializationSQLTx struct {
	Tx *sql.Tx
}

// ExecContext runs a statement on the wrapped transaction.
func (t ServiceMaterializationSQLTx) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query on the wrapped transaction.
func (t ServiceMaterializationSQLTx) QueryRowContext(
	ctx context.Context,
	query string,
	args ...any,
) ServiceMaterializationRow {
	return t.Tx.QueryRowContext(ctx, query, args...)
}

// Commit commits the wrapped transaction.
func (t ServiceMaterializationSQLTx) Commit() error { return t.Tx.Commit() }

// Rollback rolls back the wrapped transaction.
func (t ServiceMaterializationSQLTx) Rollback() error { return t.Tx.Rollback() }

// PostgresServiceMaterializationWriter commits service-scope generation lineage
// and the generation-stable ownership snapshot rows the changed-since delta
// diffs. It is additive: it never touches reducer_service_catalog_correlation
// facts or their stable_fact_key.
//
// Concurrency contract (conflict key = service_id):
//
//   - All commits for one service serialize on the partial unique index
//     service_materialization_generations_active_service_idx, which permits one
//     status='active' row per service_id. Two concurrent commits for the same
//     service cannot both leave an active row; the loser fails its insert/update
//     and the reducer retries the intent.
//   - The supersede of the prior active generation and the insert of the new
//     active generation run in one transaction, so a reader never observes zero
//     or two active generations for a service.
//   - The generation id is deterministic in the evidence set, so an identical
//     re-materialization upserts the same row (ON CONFLICT DO NOTHING) and is a
//     true no-op: no supersession, no snapshot churn, no false delta.
type PostgresServiceMaterializationWriter struct {
	DB  ServiceMaterializationBeginner
	Now func() time.Time
}

// WriteServiceMaterialization commits one service's ownership evidence as a new
// active generation, supersedes the prior active generation when the evidence
// changed, and writes the generation-stable snapshot rows. A repeat of an
// identical evidence set is a no-op.
func (w PostgresServiceMaterializationWriter) WriteServiceMaterialization(
	ctx context.Context,
	write ServiceMaterializationWrite,
) (ServiceMaterializationWriteResult, error) {
	if w.DB == nil {
		return ServiceMaterializationWriteResult{}, fmt.Errorf("service materialization database is required")
	}
	if err := validateServiceMaterializationWrite(write); err != nil {
		return ServiceMaterializationWriteResult{}, err
	}

	now := reducerWriterNow(w.Now)
	generationID := serviceMaterializationGenerationID(write)
	rows := normalizeServiceEvidence(write)

	tx, err := w.DB.BeginServiceMaterializationTx(ctx)
	if err != nil {
		return ServiceMaterializationWriteResult{}, fmt.Errorf("begin service materialization transaction: %w", err)
	}
	result, err := w.commit(ctx, tx, write, generationID, rows, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return ServiceMaterializationWriteResult{}, fmt.Errorf("%w; rollback: %v", err, rbErr)
		}
		return ServiceMaterializationWriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ServiceMaterializationWriteResult{}, fmt.Errorf("commit service materialization transaction: %w", err)
	}
	return result, nil
}

func (w PostgresServiceMaterializationWriter) commit(
	ctx context.Context,
	tx ServiceMaterializationTx,
	write ServiceMaterializationWrite,
	generationID string,
	rows []serviceEvidenceRow,
	now time.Time,
) (ServiceMaterializationWriteResult, error) {
	// Insert the new generation as pending first. A pending row never collides
	// with the single-active-per-service partial unique index, so the prior
	// active generation can stay active until it is superseded in this same
	// transaction. A deterministic generation id makes a repeat insert affect
	// zero rows (ON CONFLICT DO NOTHING), which is the idempotent no-op signal.
	inserted, err := w.insertGeneration(ctx, tx, write, generationID, now)
	if err != nil {
		return ServiceMaterializationWriteResult{}, err
	}
	if !inserted {
		// Identical evidence set already committed: idempotent no-op. The active
		// pointer and snapshot rows already reflect this generation.
		return ServiceMaterializationWriteResult{GenerationID: generationID, Committed: false}, nil
	}

	// Retire the prior active generation, then promote the new pending generation
	// to active. Ordering supersede-before-activate keeps at most one active row
	// per service at every step, so the partial unique index is never violated.
	supersededIDs, err := w.supersedePriorActive(ctx, tx, write.ServiceID, generationID, now)
	if err != nil {
		return ServiceMaterializationWriteResult{}, err
	}
	if err := w.activateGeneration(ctx, tx, write.ServiceID, generationID, now); err != nil {
		return ServiceMaterializationWriteResult{}, err
	}

	for _, row := range rows {
		if err := w.insertEvidenceRow(ctx, tx, write.ServiceID, generationID, row, now); err != nil {
			return ServiceMaterializationWriteResult{}, err
		}
	}

	return ServiceMaterializationWriteResult{
		GenerationID:  generationID,
		Committed:     true,
		EvidenceRows:  len(rows),
		SupersededIDs: supersededIDs,
	}, nil
}

func (w PostgresServiceMaterializationWriter) insertGeneration(
	ctx context.Context,
	tx ServiceMaterializationTx,
	write ServiceMaterializationWrite,
	generationID string,
	now time.Time,
) (bool, error) {
	res, err := tx.ExecContext(
		ctx,
		insertServiceMaterializationGenerationQuery,
		generationID,
		write.ServiceID,
		serviceMaterializationTriggerKind(write.TriggerKind),
		nullableString(write.IntentID),
		now,
		now,
		ServiceMaterializationStatusPending,
		nil,
		serviceMaterializationGenerationPayload(write),
	)
	if err != nil {
		return false, fmt.Errorf("insert service materialization generation: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert service materialization generation rows affected: %w", err)
	}
	return affected > 0, nil
}

func (w PostgresServiceMaterializationWriter) supersedePriorActive(
	ctx context.Context,
	tx ServiceMaterializationTx,
	serviceID, generationID string,
	now time.Time,
) ([]string, error) {
	row := tx.QueryRowContext(
		ctx,
		supersedePriorServiceGenerationQuery,
		serviceID,
		generationID,
		now,
	)
	var superseded sql.NullString
	if scanErr := row.Scan(&superseded); scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("supersede prior service generation: %w", scanErr)
	}
	if !superseded.Valid || superseded.String == "" {
		return nil, nil
	}
	return []string{superseded.String}, nil
}

// activateGeneration promotes the freshly inserted pending generation to active.
// It runs after the prior active generation is superseded, so at most one active
// row per service exists at any step and the partial unique index is never
// violated.
func (w PostgresServiceMaterializationWriter) activateGeneration(
	ctx context.Context,
	tx ServiceMaterializationTx,
	serviceID, generationID string,
	now time.Time,
) error {
	if _, err := tx.ExecContext(
		ctx,
		activateServiceGenerationQuery,
		serviceID,
		generationID,
		now,
	); err != nil {
		return fmt.Errorf("activate service materialization generation: %w", err)
	}
	return nil
}

func (w PostgresServiceMaterializationWriter) insertEvidenceRow(
	ctx context.Context,
	tx ServiceMaterializationTx,
	serviceID, generationID string,
	row serviceEvidenceRow,
	now time.Time,
) error {
	payloadJSON, err := json.Marshal(canonicalizeEvidencePayload(row.payload))
	if err != nil {
		return fmt.Errorf("marshal service %s evidence payload: %w", row.family, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		insertServiceEvidenceSnapshotQuery,
		generationID,
		serviceID,
		row.family,
		row.evidenceKey,
		row.payloadHash,
		row.tombstone,
		now,
		payloadJSON,
	); err != nil {
		return fmt.Errorf("insert service evidence snapshot: %w", err)
	}
	return nil
}

func serviceMaterializationTriggerKind(triggerKind string) string {
	if triggerKind == "" {
		return "service_catalog_correlation"
	}
	return triggerKind
}

func serviceMaterializationGenerationPayload(write ServiceMaterializationWrite) []byte {
	payload := map[string]any{
		"service_id":         write.ServiceID,
		"intent_id":          write.IntentID,
		"ownership_count":    len(write.Ownership),
		"deployment_count":   len(write.Deployment),
		"runtime_count":      len(write.Runtime),
		"dependencies_count": len(write.Dependencies),
		"docs_count":         len(write.Docs),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
