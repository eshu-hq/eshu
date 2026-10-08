// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhookstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// WebhookTriggerStore persists provider webhook intake decisions for later
// targeted repository refresh handoff.
type WebhookTriggerStore struct {
	database db.ExecQueryer
}

// NewWebhookTriggerStore constructs a Postgres-backed webhook trigger store.
func NewWebhookTriggerStore(database db.ExecQueryer) *WebhookTriggerStore {
	return &WebhookTriggerStore{database: database}
}

// WebhookTriggerSchemaSQL returns the DDL for the webhook trigger store.
func WebhookTriggerSchemaSQL() string {
	return webhookTriggerSchemaSQL
}

// EnsureSchema applies the webhook trigger schema.
func (s *WebhookTriggerStore) EnsureSchema(ctx context.Context) error {
	if s.database == nil {
		return errors.New("webhook trigger store database is required")
	}
	if _, err := s.database.ExecContext(ctx, webhookTriggerSchemaSQL); err != nil {
		return fmt.Errorf("ensure webhook trigger schema: %w", err)
	}
	return nil
}

// StoreTrigger persists one normalized trigger and deduplicates by refresh
// identity. Webhook payloads remain trigger evidence only; this method does not
// mark graph or repository truth fresh.
func (s *WebhookTriggerStore) StoreTrigger(
	ctx context.Context,
	trigger webhook.Trigger,
	receivedAt time.Time,
) (webhook.StoredTrigger, error) {
	if s.database == nil {
		return webhook.StoredTrigger{}, errors.New("webhook trigger store database is required")
	}
	stored, err := prepareStoredTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredTrigger{}, err
	}
	rows, err := s.database.QueryContext(
		ctx,
		storeWebhookTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.RefreshKey,
		string(stored.Provider),
		string(stored.EventKind),
		string(stored.Decision),
		string(stored.Reason),
		stored.DeliveryID,
		stored.RepositoryExternalID,
		stored.RepositoryFullName,
		stored.DefaultBranch,
		stored.Ref,
		stored.BeforeSHA,
		stored.TargetSHA,
		stored.Action,
		stored.Sender,
		stored.PullRequestNumber,
		stored.PullRequestURL,
		stored.PullRequestTitle,
		string(stored.Status),
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
		}
		return webhook.StoredTrigger{}, errors.New("store webhook trigger returned no row")
	}
	stored, err = scanStoredWebhookTrigger(rows)
	if err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return webhook.StoredTrigger{}, fmt.Errorf("store webhook trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for a compatibility
// handoff actor and returns the claimed rows.
func (s *WebhookTriggerStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
) ([]webhook.StoredTrigger, error) {
	if s.database == nil {
		return nil, errors.New("webhook trigger store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("webhook trigger claim owner is required")
	}
	if limit <= 0 {
		return nil, errors.New("webhook trigger claim limit must be positive")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("webhook trigger claimed_at is required")
	}

	rows, err := s.database.QueryContext(ctx, claimQueuedWebhookTriggersQuery, limit, owner, claimedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim webhook triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	triggers := make([]webhook.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanStoredWebhookTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim webhook triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim webhook triggers: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records that claimed triggers were handed to the
// repository refresh selector. Each completion carries the claim fencing
// token the claim returned; a holder whose lease expired and was reaped
// affects zero rows instead of completing the new owner's claim (#7661).
func (s *WebhookTriggerStore) MarkTriggersHandedOff(ctx context.Context, triggers []webhook.StoredTrigger, handedOffAt time.Time) error {
	if s.database == nil {
		return errors.New("webhook trigger store database is required")
	}
	if len(triggers) == 0 {
		return errors.New("webhook triggers are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("webhook trigger handed_off_at is required")
	}
	args := webhookFencedTriggerArgs(triggers, handedOffAt.UTC())
	if _, err := s.database.ExecContext(ctx, buildMarkWebhookTriggersHandedOffQuery(len(triggers)), args...); err != nil {
		return fmt.Errorf("mark webhook triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records a failed compatibility handoff so claimed
// triggers do not stay invisible to operators. Completions are fenced by
// the claim token exactly like MarkTriggersHandedOff (#7661).
func (s *WebhookTriggerStore) MarkTriggersFailed(
	ctx context.Context,
	triggers []webhook.StoredTrigger,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.database == nil {
		return errors.New("webhook trigger store database is required")
	}
	if len(triggers) == 0 {
		return errors.New("webhook triggers are required")
	}
	if failedAt.IsZero() {
		return errors.New("webhook trigger failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("webhook trigger failure class is required")
	}
	args := webhookFencedTriggerArgs(triggers, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.database.ExecContext(
		ctx,
		buildMarkWebhookTriggersFailedQuery(len(triggers)),
		args...,
	); err != nil {
		return fmt.Errorf("mark webhook triggers failed: %w", err)
	}
	return nil
}

// ReapExpiredTriggerClaims recovers claimed rows whose claimed_at predates
// staleBefore: rows below maxAttempts go back to queued, rows at or past
// the cap go to failed with the claim_lease_exhausted reason (#7661). Two
// reclaimers racing split the stale set via SKIP LOCKED; callers pass a
// positive per-statement limit, so one sweep touches at most 2×limit rows
// across the requeue and exhaust statements.
func (s *WebhookTriggerStore) ReapExpiredTriggerClaims(
	ctx context.Context,
	staleBefore time.Time,
	maxAttempts int,
	limit int,
	asOf time.Time,
) (requeued []webhook.StoredTrigger, exhausted []webhook.StoredTrigger, err error) {
	if s.database == nil {
		return nil, nil, errors.New("webhook trigger store database is required")
	}
	if staleBefore.IsZero() {
		return nil, nil, errors.New("webhook trigger reap cutoff is required")
	}
	if maxAttempts <= 0 {
		return nil, nil, errors.New("webhook trigger max attempts must be positive")
	}
	if limit <= 0 {
		return nil, nil, errors.New("webhook trigger reap limit must be positive")
	}
	if asOf.IsZero() {
		return nil, nil, errors.New("webhook trigger reap timestamp is required")
	}
	requeued, err = scanWebhookTriggers(s.database.QueryContext(ctx, reapStaleWebhookTriggerClaimsQuery, staleBefore.UTC(), maxAttempts, limit, asOf.UTC()))
	if err != nil {
		return nil, nil, fmt.Errorf("reap stale webhook trigger claims: %w", err)
	}
	exhausted, err = scanWebhookTriggers(s.database.QueryContext(ctx, exhaustStaleWebhookTriggerClaimsQuery, staleBefore.UTC(), maxAttempts, limit, asOf.UTC()))
	if err != nil {
		return nil, nil, fmt.Errorf("exhaust stale webhook trigger claims: %w", err)
	}
	return requeued, exhausted, nil
}

// CountStaleClaims counts the rows stuck in claimed past staleBefore: the
// stuck-claim gauge's source (#7661).
func (s *WebhookTriggerStore) CountStaleClaims(ctx context.Context, staleBefore time.Time) (int64, error) {
	if s.database == nil {
		return 0, errors.New("webhook trigger store database is required")
	}
	if staleBefore.IsZero() {
		return 0, errors.New("webhook trigger reap cutoff is required")
	}
	rows, err := s.database.QueryContext(ctx, countStaleWebhookTriggerClaimsQuery, staleBefore.UTC())
	if err != nil {
		return 0, fmt.Errorf("count stale webhook trigger claims: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, errors.New("count stale webhook trigger claims: no rows")
	}
	var count int64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("count stale webhook trigger claims: %w", err)
	}
	return count, nil
}

func scanWebhookTriggers(rows db.Rows, err error) ([]webhook.StoredTrigger, error) {
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]webhook.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanStoredWebhookTrigger(rows)
		if err != nil {
			return nil, err
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return triggers, nil
}

func prepareStoredTrigger(trigger webhook.Trigger, receivedAt time.Time) (webhook.StoredTrigger, error) {
	if receivedAt.IsZero() {
		return webhook.StoredTrigger{}, errors.New("webhook trigger received_at is required")
	}
	deliveryKey := webhookDeliveryKey(trigger)
	refreshKey := webhookRefreshKey(trigger)
	if deliveryKey == "" {
		return webhook.StoredTrigger{}, errors.New("webhook trigger delivery key is required")
	}
	if refreshKey == "" {
		return webhook.StoredTrigger{}, errors.New("webhook trigger refresh key is required")
	}
	status := webhook.TriggerStatusQueued
	if trigger.Decision == webhook.DecisionIgnored {
		status = webhook.TriggerStatusIgnored
	}
	return webhook.StoredTrigger{
		Trigger:     trigger,
		TriggerID:   facts.StableID("WebhookRefreshTrigger", map[string]any{"refresh_key": refreshKey}),
		DeliveryKey: deliveryKey,
		RefreshKey:  refreshKey,
		Status:      status,
		ReceivedAt:  receivedAt.UTC(),
		UpdatedAt:   receivedAt.UTC(),
	}, nil
}

func webhookDeliveryKey(trigger webhook.Trigger) string {
	parts := []string{
		string(trigger.Provider),
		strings.TrimSpace(trigger.DeliveryID),
		strings.TrimSpace(trigger.RepositoryExternalID),
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ":")
}

func webhookRefreshKey(trigger webhook.Trigger) string {
	parts := []string{
		string(trigger.Provider),
		strings.TrimSpace(trigger.RepositoryExternalID),
		strings.TrimSpace(trigger.DefaultBranch),
		strings.TrimSpace(trigger.TargetSHA),
	}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return strings.Join(parts, ":")
}

func scanStoredWebhookTrigger(rows db.Rows) (webhook.StoredTrigger, error) {
	var stored webhook.StoredTrigger
	var provider, eventKind, decision, reason, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.RefreshKey,
		&provider,
		&eventKind,
		&decision,
		&reason,
		&stored.DeliveryID,
		&stored.RepositoryExternalID,
		&stored.RepositoryFullName,
		&stored.DefaultBranch,
		&stored.Ref,
		&stored.BeforeSHA,
		&stored.TargetSHA,
		&stored.Action,
		&stored.Sender,
		&stored.PullRequestNumber,
		&stored.PullRequestURL,
		&stored.PullRequestTitle,
		&status,
		&stored.DuplicateCount,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
		&stored.ClaimFencingToken,
	); err != nil {
		return webhook.StoredTrigger{}, err
	}
	stored.Provider = webhook.Provider(provider)
	stored.EventKind = webhook.EventKind(eventKind)
	stored.Decision = webhook.Decision(decision)
	stored.Reason = webhook.DecisionReason(reason)
	stored.Status = webhook.TriggerStatus(status)
	return stored, nil
}

func buildMarkWebhookTriggersHandedOffQuery(rowCount int) string {
	timestampParam := rowCount*2 + 1
	return fmt.Sprintf(
		markWebhookTriggersHandedOffQueryFormat,
		timestampParam,
		timestampParam,
		webhookFencedTriggerPlaceholders(rowCount),
	)
}

func buildMarkWebhookTriggersFailedQuery(rowCount int) string {
	failureClassParam := rowCount*2 + 1
	failureMessageParam := rowCount*2 + 2
	timestampParam := rowCount*2 + 3
	return fmt.Sprintf(
		markWebhookTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		webhookFencedTriggerPlaceholders(rowCount),
	)
}

// webhookFencedTriggerPlaceholders returns one "($n, $n+1)" pair per row
// for the VALUES(trigger_id, fencing_token) clause the mark-handed-off/
// failed query formats join against (#7661).
func webhookFencedTriggerPlaceholders(rowCount int) string {
	pairs := make([]string, rowCount)
	for i := range pairs {
		idParam := i*2 + 1
		tokenParam := i*2 + 2
		pairs[i] = fmt.Sprintf("($%d, $%d::bigint)", idParam, tokenParam)
	}
	return strings.Join(pairs, ", ")
}

// webhookFencedTriggerArgs interleaves each trigger's id and fencing token
// (matching webhookFencedTriggerPlaceholders's pairing) ahead of any
// trailing args (timestamps, failure class/message).
func webhookFencedTriggerArgs(triggers []webhook.StoredTrigger, extra ...any) []any {
	args := make([]any, 0, len(triggers)*2+len(extra))
	for _, trigger := range triggers {
		args = append(args, trigger.TriggerID, trigger.ClaimFencingToken)
	}
	return append(args, extra...)
}
