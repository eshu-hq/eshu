// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	payloadstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const publicAdvisoryStageQuery = `
SELECT eshu_security.stage_public_advisory_generation_v1(
    $1,$2,$3,$4,$5,$6,$7,$8::uuid)`

const publicAdvisoryRecordFactsQuery = `
SELECT eshu_security.record_public_advisory_facts_v1(
    $1,$2::jsonb,$3,$4::uuid)`

const publicAdvisoryCompleteQuery = `
SELECT eshu_security.complete_public_advisory_generation_v1(
    $1,$2,$3,$4,$5::uuid)`

const publicAdvisoryPublishQuery = `
SELECT eshu_security.publish_public_advisory_generation_v1(
    $1,$2,$3,$4,$5,$6,$7,$8::uuid)`

// ProtectedPublicAdvisoryAuthority calls fixed protected SQL capabilities
// through a dedicated typed-advisory producer pool. The role revision and
// database identity come from that pool's startup-attested role contract.
// Constructing this adapter does not itself grant the pool any capability.
type ProtectedPublicAdvisoryAuthority struct {
	beginner         db.Beginner
	roleRevision     int64
	databaseIdentity string
}

// NewProtectedPublicAdvisoryAuthority requires a transactional, producer-bound
// database handle and the exact startup-attested role policy identity.
func NewProtectedPublicAdvisoryAuthority(database db.ExecQueryer,
	roleRevision int64, databaseIdentity string,
) (ProtectedPublicAdvisoryAuthority, error) {
	beginner, ok := database.(db.Beginner)
	if !ok || roleRevision <= 0 || strings.TrimSpace(databaseIdentity) == "" {
		return ProtectedPublicAdvisoryAuthority{}, errors.New("public advisory authority requires a transactional producer role contract")
	}
	return ProtectedPublicAdvisoryAuthority{
		beginner: beginner, roleRevision: roleRevision, databaseIdentity: databaseIdentity,
	}, nil
}

var _ PublicAdvisoryGenerationAuthority = ProtectedPublicAdvisoryAuthority{}

// StagePublicAdvisoryGeneration pins the current producer grant and
// registration before the ordinary claim or fact rows are locked.
func (a ProtectedPublicAdvisoryAuthority) StagePublicAdvisoryGeneration(
	ctx context.Context, tx db.ExecQueryer, mutation workflow.ClaimMutation,
	scopeID, generationID string,
) (string, error) {
	if tx == nil || scopeID == "" || generationID == "" {
		return "", errors.New("public advisory stage identity invalid")
	}
	var receiptID string
	if err := scanProtectedAdvisoryScalar(ctx, tx, publicAdvisoryStageQuery, &receiptID,
		scopeID, generationID, mutation.WorkItemID, mutation.ClaimID,
		mutation.FencingToken, mutation.OwnerID, a.roleRevision, a.databaseIdentity); err != nil {
		return "", fmt.Errorf("stage public advisory generation: %w", err)
	}
	if receiptID == "" {
		return "", errors.New("public advisory stage returned empty receipt")
	}
	return receiptID, nil
}

// RecordAcceptedPublicAdvisoryFacts binds the exact accepted fact batch to the
// staged producer receipt. The caller rejects any fencing-filtered batch
// before this method; SQL rechecks each persisted JSONB payload and identity.
func (a ProtectedPublicAdvisoryAuthority) RecordAcceptedPublicAdvisoryFacts(
	ctx context.Context, tx db.ExecQueryer, receiptID string, batch []facts.Envelope,
) error {
	if tx == nil || receiptID == "" {
		return errors.New("public advisory accepted batch identity invalid")
	}
	encoded, err := marshalAcceptedPublicAdvisoryFacts(batch)
	if err != nil {
		return err
	}
	var accepted int
	if err := scanProtectedAdvisoryScalar(ctx, tx, publicAdvisoryRecordFactsQuery,
		&accepted, receiptID, string(encoded), a.roleRevision, a.databaseIdentity); err != nil {
		return fmt.Errorf("record public advisory accepted facts: %w", err)
	}
	if accepted != len(batch) {
		return fmt.Errorf("public advisory recorded %d of %d accepted facts", accepted, len(batch))
	}
	return nil
}

func marshalAcceptedPublicAdvisoryFacts(batch []facts.Envelope) ([]byte, error) {
	if len(batch) == 0 || len(batch) > factBatchSize {
		return nil, errors.New("public advisory accepted fact batch size invalid")
	}
	type entry struct {
		FactID   string          `json:"fact_id"`
		FactKind string          `json:"fact_kind"`
		Payload  json.RawMessage `json:"payload"`
	}
	entries := make([]entry, 0, len(batch))
	for _, envelope := range batch {
		if envelope.FactID == "" || envelope.FactKind == "" {
			return nil, errors.New("public advisory accepted fact identity invalid")
		}
		payload, err := payloadstore.MarshalPayload(envelope.Payload)
		if err != nil {
			return nil, fmt.Errorf("encode public advisory fact payload: %w", err)
		}
		entries = append(entries, entry{
			FactID: envelope.FactID, FactKind: envelope.FactKind,
			Payload: json.RawMessage(payload),
		})
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("encode public advisory accepted batch: %w", err)
	}
	if len(encoded) > 4<<20 {
		return nil, errors.New("public advisory accepted fact batch exceeds protected limit")
	}
	return encoded, nil
}

// RecordCompletePublicAdvisoryGeneration seals the actual fact set and source
// snapshot marker in the original fact transaction before it can commit.
func (a ProtectedPublicAdvisoryAuthority) RecordCompletePublicAdvisoryGeneration(
	ctx context.Context, tx db.ExecQueryer, receiptID, scopeID, generationID string,
) error {
	if tx == nil || receiptID == "" || scopeID == "" || generationID == "" {
		return errors.New("public advisory completion identity invalid")
	}
	var complete bool
	if err := scanProtectedAdvisoryScalar(ctx, tx, publicAdvisoryCompleteQuery,
		&complete, receiptID, scopeID, generationID,
		a.roleRevision, a.databaseIdentity); err != nil {
		return fmt.Errorf("complete public advisory generation: %w", err)
	}
	if !complete {
		return errors.New("public advisory generation did not complete")
	}
	return nil
}

// PublishCompletePublicAdvisoryGeneration activates an already complete
// receipt in a separate short transaction, under the same live claim fence.
func (a ProtectedPublicAdvisoryAuthority) PublishCompletePublicAdvisoryGeneration(
	ctx context.Context, mutation workflow.ClaimMutation, scopeID, generationID string,
) (bool, error) {
	if a.beginner == nil || scopeID == "" || generationID == "" {
		return false, errors.New("public advisory publication identity invalid")
	}
	tx, err := a.beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin public advisory publication: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var published bool
	if err := scanProtectedAdvisoryScalar(ctx, tx, publicAdvisoryPublishQuery,
		&published, scopeID, generationID, mutation.WorkItemID,
		mutation.ClaimID, mutation.FencingToken, mutation.OwnerID,
		a.roleRevision, a.databaseIdentity); err != nil {
		return false, fmt.Errorf("publish public advisory generation: %w", err)
	}
	if !published {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit public advisory publication: %w", err)
	}
	committed = true
	return true, nil
}

func scanProtectedAdvisoryScalar(ctx context.Context, queryer db.Queryer,
	query string, destination any, args ...any,
) error {
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	if !rows.Next() {
		readErr := rows.Err()
		closeErr := rows.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		return errors.New("protected advisory capability returned no row")
	}
	if err := rows.Scan(destination); err != nil {
		_ = rows.Close()
		return err
	}
	if rows.Next() {
		_ = rows.Close()
		return errors.New("protected advisory capability returned multiple rows")
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	return errors.Join(readErr, closeErr)
}
