// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package actionsstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

const (
	maxClaimLimit    = 100
	maxClaimAttempts = 8
)

// Store persists Actions webhook deliveries and fenced handoff state.
type Store struct{ database db.ExecQueryer }

// NewStore constructs one Actions delivery inbox over the shared Postgres pool.
func NewStore(database db.ExecQueryer) *Store { return &Store{database: database} }

// EnsureSchema applies the same DDL carried by migration 236.
func (s *Store) EnsureSchema(ctx context.Context) error {
	if s == nil || s.database == nil {
		return errors.New("actions delivery store database is required")
	}
	if _, err := s.database.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("ensure actions delivery schema: %w", err)
	}
	return nil
}

// Store records one immutable delivery. A duplicate with different bounded
// identity fails closed; a duplicate never changes queue or terminal status.
func (s *Store) Store(ctx context.Context, trigger webhook.ActionsTrigger, receivedAt time.Time) (webhook.StoredActionsTrigger, error) {
	if s == nil || s.database == nil {
		return webhook.StoredActionsTrigger{}, errors.New("actions delivery store database is required")
	}
	if err := validateTrigger(trigger, receivedAt); err != nil {
		return webhook.StoredActionsTrigger{}, err
	}
	_, err := s.database.ExecContext(ctx, insertSQL, trigger.DeliveryID, trigger.EventKind, trigger.Action, trigger.RepositoryID, trigger.Repository, trigger.RunID, trigger.JobID, receivedAt.UTC())
	if err != nil {
		return webhook.StoredActionsTrigger{}, fmt.Errorf("store actions delivery: %w", err)
	}
	rows, err := s.database.QueryContext(ctx, `SELECT `+selectColumns+` FROM cicd_actions_deliveries WHERE delivery_id=$1`, trigger.DeliveryID)
	if err != nil {
		return webhook.StoredActionsTrigger{}, fmt.Errorf("read actions delivery: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return webhook.StoredActionsTrigger{}, fmt.Errorf("read actions delivery: %w", err)
		}
		return webhook.StoredActionsTrigger{}, errors.New("stored actions delivery is missing")
	}
	stored, err := scan(rows)
	if err != nil {
		return webhook.StoredActionsTrigger{}, err
	}
	if err = rows.Err(); err != nil {
		return webhook.StoredActionsTrigger{}, fmt.Errorf("read actions delivery: %w", err)
	}
	if stored.ActionsTrigger != trigger {
		return webhook.StoredActionsTrigger{}, errors.New("conflicting delivery identity")
	}
	return stored, nil
}

// HasPendingRepository reports queued or claimed work for one exact repository,
// including a blocked delivery whose visible_at retry time is in the future.
func (s *Store) HasPendingRepository(ctx context.Context, repository string) (bool, error) {
	if s == nil || s.database == nil {
		return false, errors.New("actions delivery store database is required")
	}
	if repository == "" {
		return false, errors.New("actions repository is required")
	}
	rows, err := s.database.QueryContext(ctx, `SELECT EXISTS (SELECT 1 FROM cicd_actions_deliveries
  WHERE repository=$1 AND status IN ('queued','claimed'))`, repository)
	if err != nil {
		return false, fmt.Errorf("read pending actions repository: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return false, fmt.Errorf("read pending actions repository: %w", err)
		}
		return false, errors.New("pending actions repository query returned no row")
	}
	var pending bool
	if err = rows.Scan(&pending); err != nil {
		return false, fmt.Errorf("scan pending actions repository: %w", err)
	}
	if err = rows.Err(); err != nil {
		return false, fmt.Errorf("read pending actions repository: %w", err)
	}
	return pending, nil
}

// Claim leases at most 100 queued deliveries, oldest first. A new lease bumps
// the token so a stale holder cannot ACK after expiry and reclaim.
func (s *Store) Claim(ctx context.Context, owner string, at time.Time, limit int, lease time.Duration) ([]webhook.StoredActionsTrigger, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("actions delivery store database is required")
	}
	if strings.TrimSpace(owner) == "" {
		return nil, errors.New("actions claim owner is required")
	}
	if at.IsZero() || lease <= 0 || lease > 10*time.Minute {
		return nil, errors.New("actions claim requires time and lease within 10m")
	}
	if limit < 1 || limit > maxClaimLimit {
		return nil, fmt.Errorf("actions claim limit must be 1..%d", maxClaimLimit)
	}
	rows, err := s.database.QueryContext(ctx, claimSQL, limit, at.UTC().Add(lease), at.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim actions deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]webhook.StoredActionsTrigger, 0, limit)
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("claim actions deliveries: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].DeliveryID < out[j].DeliveryID
		}
		return out[i].ReceivedAt.Before(out[j].ReceivedAt)
	})
	return out, nil
}

// Reap returns expired claims to the queue, or fails an exhausted delivery.
// The result counts resulting states successfully returned by the reap query.
func (s *Store) Reap(ctx context.Context, asOf time.Time, limit int) (webhook.ActionsReapResult, error) {
	if s == nil || s.database == nil {
		return webhook.ActionsReapResult{}, errors.New("actions delivery store database is required")
	}
	if asOf.IsZero() || limit < 1 || limit > maxClaimLimit {
		return webhook.ActionsReapResult{}, errors.New("actions reap requires time and limit 1..100")
	}
	rows, err := s.database.QueryContext(ctx, reapSQL, asOf.UTC(), limit, maxClaimAttempts)
	if err != nil {
		return webhook.ActionsReapResult{}, fmt.Errorf("reap actions deliveries: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result webhook.ActionsReapResult
	for rows.Next() {
		var status string
		if err = rows.Scan(&status); err != nil {
			return webhook.ActionsReapResult{}, fmt.Errorf("scan reaped actions delivery: %w", err)
		}
		switch status {
		case string(webhook.TriggerStatusQueued):
			result.Requeued++
		case string(webhook.TriggerStatusFailed):
			result.Exhausted++
		default:
			return webhook.ActionsReapResult{}, fmt.Errorf("reaped actions delivery has unexpected status %q", status)
		}
	}
	if err = rows.Err(); err != nil {
		return webhook.ActionsReapResult{}, fmt.Errorf("reap actions deliveries: %w", err)
	}
	return result, nil
}

// Ack records handoff only for an unexpired, current claim token.
func (s *Store) Ack(ctx context.Context, claim webhook.StoredActionsTrigger, at time.Time) (bool, error) {
	return s.complete(ctx, ackSQL, claim, at)
}

// Release retries a claimed delivery after blocked or failed admission.
func (s *Store) Release(ctx context.Context, claim webhook.StoredActionsTrigger, at time.Time, class string) (bool, error) {
	if class != "blocked" && class != "admission_failed" && class != "no_active_instance" {
		return false, errors.New("invalid actions retry class")
	}
	return s.complete(ctx, releaseSQL, claim, at, maxClaimAttempts, class)
}

// Fail records a permanent unauthorized or malformed target without retry.
func (s *Store) Fail(ctx context.Context, claim webhook.StoredActionsTrigger, at time.Time, class string) (bool, error) {
	if class != "unauthorized_target" && class != "invalid_target" {
		return false, errors.New("invalid actions failure class")
	}
	return s.complete(ctx, failSQL, claim, at, class)
}

func (s *Store) complete(ctx context.Context, query string, claim webhook.StoredActionsTrigger, at time.Time, extra ...any) (bool, error) {
	if s == nil || s.database == nil {
		return false, errors.New("actions delivery store database is required")
	}
	if at.IsZero() || claim.DeliveryID == "" || claim.ClaimFencingToken <= 0 {
		return false, errors.New("actions completion requires delivery, fence and time")
	}
	args := append([]any{claim.DeliveryID, claim.ClaimFencingToken, at.UTC()}, extra...)
	result, err := s.database.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("complete actions delivery: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("count actions delivery completion: %w", err)
	}
	return changed == 1, nil
}

func validateTrigger(trigger webhook.ActionsTrigger, receivedAt time.Time) error {
	if receivedAt.IsZero() {
		return errors.New("actions received_at is required")
	}
	if trigger.DeliveryID == "" || len(trigger.DeliveryID) > 256 {
		return errors.New("actions delivery_id must contain 1..256 bytes")
	}
	if trigger.EventKind != "workflow_run" && trigger.EventKind != "workflow_job" {
		return errors.New("actions event_kind is unsupported")
	}
	if trigger.Action == "" || len(trigger.Action) > 64 {
		return errors.New("actions action must contain 1..64 bytes")
	}
	if trigger.RepositoryID == "" || trigger.Repository == "" || trigger.RunID == "" {
		return errors.New("actions repository and run identifiers are required")
	}
	if trigger.EventKind == "workflow_job" && trigger.JobID == "" {
		return errors.New("actions workflow_job.id is required")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (webhook.StoredActionsTrigger, error) {
	var item webhook.StoredActionsTrigger
	var expires sql.NullTime
	var status string
	err := row.Scan(&item.DeliveryID, &item.EventKind, &item.Action, &item.RepositoryID, &item.Repository, &item.RunID, &item.JobID, &item.HandoffKey, &status, &item.ClaimFencingToken, &item.ClaimAttempts, &expires, &item.ReceivedAt, &item.UpdatedAt)
	if err != nil {
		return webhook.StoredActionsTrigger{}, fmt.Errorf("scan actions delivery: %w", err)
	}
	item.Status = webhook.TriggerStatus(status)
	if expires.Valid {
		item.ClaimExpiresAt = expires.Time
	}
	return item, nil
}
