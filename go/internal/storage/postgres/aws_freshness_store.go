// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
)

// AWSFreshnessStore persists AWS event-driven refresh triggers for later
// workflow handoff.
type AWSFreshnessStore struct {
	db ExecQueryer
}

// NewAWSFreshnessStore constructs a Postgres-backed AWS freshness store.
func NewAWSFreshnessStore(db ExecQueryer) *AWSFreshnessStore {
	return &AWSFreshnessStore{db: db}
}

// AWSFreshnessSchemaSQL returns the DDL for the AWS freshness trigger store.
func AWSFreshnessSchemaSQL() string {
	return awsFreshnessSchemaSQL
}

// EnsureSchema applies the AWS freshness trigger schema.
func (s *AWSFreshnessStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	if _, err := s.db.ExecContext(ctx, awsFreshnessSchemaSQL); err != nil {
		return fmt.Errorf("ensure AWS freshness schema: %w", err)
	}
	return nil
}

// StoreTrigger persists and coalesces one normalized AWS freshness event.
func (s *AWSFreshnessStore) StoreTrigger(
	ctx context.Context,
	trigger freshness.Trigger,
	receivedAt time.Time,
) (freshness.StoredTrigger, error) {
	if s.db == nil {
		return freshness.StoredTrigger{}, errors.New("AWS freshness store database is required")
	}
	stored, err := freshness.NewStoredTrigger(trigger, receivedAt)
	if err != nil {
		return freshness.StoredTrigger{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		storeAWSFreshnessTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(stored.Kind),
		stored.EventID,
		stored.AccountID,
		stored.Region,
		stored.ServiceKind,
		stored.ResourceType,
		stored.ResourceID,
		string(stored.Status),
		stored.ObservedAt,
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
		}
		return freshness.StoredTrigger{}, errors.New("store AWS freshness trigger returned no row")
	}
	stored, err = scanAWSFreshnessTrigger(rows)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store AWS freshness trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for one handoff actor.
// The claim carries a claim_expires_at lease (claimedAt+leaseDuration) so a
// mid-batch handoff abort or coordinator crash cannot strand the row at
// 'claimed' forever; a later ReapExpiredTriggerClaims call requeues it once
// the lease expires (#4576).
func (s *AWSFreshnessStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
	leaseDuration time.Duration,
) ([]freshness.StoredTrigger, error) {
	if s.db == nil {
		return nil, errors.New("AWS freshness store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("AWS freshness claim owner is required")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("AWS freshness claimed_at is required")
	}
	if limit <= 0 {
		return nil, errors.New("AWS freshness claim limit must be positive")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("AWS freshness claim lease duration must be positive")
	}
	claimedAtUTC := claimedAt.UTC()
	rows, err := s.db.QueryContext(
		ctx,
		claimQueuedAWSFreshnessTriggersQuery,
		limit,
		owner,
		claimedAtUTC,
		claimedAtUTC.Add(leaseDuration),
	)
	if err != nil {
		return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]freshness.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanAWSFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim AWS freshness triggers: %w", err)
	}
	return triggers, nil
}

// ReapExpiredTriggerClaims reclaims 'claimed' AWS freshness triggers whose
// claim lease has expired back to 'queued', mirroring the workflow_claims
// expired-lease reclaim pattern (#4464). This is what recovers a trigger
// stranded at 'claimed' by a mid-batch handoff abort or a coordinator crash
// between claim and mark-handed-off/failed (#4576).
func (s *AWSFreshnessStore) ReapExpiredTriggerClaims(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]freshness.StoredTrigger, error) {
	if s.db == nil {
		return nil, errors.New("AWS freshness store database is required")
	}
	if asOf.IsZero() {
		return nil, errors.New("AWS freshness reap as-of time is required")
	}
	if limit <= 0 {
		return nil, errors.New("AWS freshness reap limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, reapExpiredAWSFreshnessTriggerClaimsQuery, asOf.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("reap expired AWS freshness trigger claims: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]freshness.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanAWSFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("reap expired AWS freshness trigger claims: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reap expired AWS freshness trigger claims: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records successful workflow handoff for claimed
// triggers. Each write is fenced by the trigger's ClaimFencingToken (#4576):
// a row completes only if its current claim_fencing_token still matches the
// token the caller received from ClaimQueuedTriggers, so a stale claimant
// whose lease expired and was reaped by ReapExpiredTriggerClaims — and whose
// trigger a different owner then re-claimed, bumping the token again —
// cannot complete a claim it no longer holds. It is not an error for fewer
// rows to be affected than triggers passed in: a fenced-out row is exactly
// the case this guards against, not a failure worth surfacing as one (the
// stale caller already lost the race and has nothing further to do).
func (s *AWSFreshnessStore) MarkTriggersHandedOff(ctx context.Context, triggers []freshness.StoredTrigger, handedOffAt time.Time) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	cleaned := cleanAWSFreshnessTriggerClaims(triggers)
	if len(cleaned) == 0 {
		return errors.New("AWS freshness trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("AWS freshness handed_off_at is required")
	}
	args := awsFreshnessFencedTriggerArgs(cleaned, handedOffAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkAWSFreshnessTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark AWS freshness triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records failed workflow handoff for claimed triggers.
// See MarkTriggersHandedOff's doc comment for the claim_fencing_token
// fencing rationale (#4576).
func (s *AWSFreshnessStore) MarkTriggersFailed(
	ctx context.Context,
	triggers []freshness.StoredTrigger,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.db == nil {
		return errors.New("AWS freshness store database is required")
	}
	cleaned := cleanAWSFreshnessTriggerClaims(triggers)
	if len(cleaned) == 0 {
		return errors.New("AWS freshness trigger ids are required")
	}
	if failedAt.IsZero() {
		return errors.New("AWS freshness failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("AWS freshness failure class is required")
	}
	args := awsFreshnessFencedTriggerArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkAWSFreshnessTriggersFailedQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark AWS freshness triggers failed: %w", err)
	}
	return nil
}

// scanAWSFreshnessTrigger scans a row shape ending in claim_fencing_token.
// Callers that RETURNING a row without that trailing column (none currently)
// must not use this scanner.
func scanAWSFreshnessTrigger(rows Rows) (freshness.StoredTrigger, error) {
	var stored freshness.StoredTrigger
	var kind, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.FreshnessKey,
		&kind,
		&stored.EventID,
		&stored.AccountID,
		&stored.Region,
		&stored.ServiceKind,
		&stored.ResourceType,
		&stored.ResourceID,
		&status,
		&stored.DuplicateCount,
		&stored.ObservedAt,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
		&stored.ClaimFencingToken,
	); err != nil {
		return freshness.StoredTrigger{}, err
	}
	stored.Kind = freshness.EventKind(kind)
	stored.Status = freshness.TriggerStatus(status)
	return stored, nil
}

func buildMarkAWSFreshnessTriggersHandedOffQuery(rowCount int) string {
	timestampParam := rowCount*2 + 1
	return fmt.Sprintf(markAWSFreshnessTriggersHandedOffQueryFormat, timestampParam, timestampParam, awsFreshnessFencedTriggerPlaceholders(rowCount))
}

func buildMarkAWSFreshnessTriggersFailedQuery(rowCount int) string {
	failureClassParam := rowCount*2 + 1
	failureMessageParam := rowCount*2 + 2
	timestampParam := rowCount*2 + 3
	return fmt.Sprintf(
		markAWSFreshnessTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		awsFreshnessFencedTriggerPlaceholders(rowCount),
	)
}

// cleanAWSFreshnessTriggerClaims dedupes triggers by trigger_id (keeping the
// first ClaimFencingToken seen for that id) and drops blank-id rows, mirroring
// cleanAWSFreshnessTriggerIDs's dedup behavior before the #4576 fencing
// change. A trigger with a blank id cannot be fenced (there is nothing to
// match on), so it is dropped rather than sent to the database.
func cleanAWSFreshnessTriggerClaims(triggers []freshness.StoredTrigger) []freshness.StoredTrigger {
	cleaned := make([]freshness.StoredTrigger, 0, len(triggers))
	seen := make(map[string]struct{}, len(triggers))
	for _, trigger := range triggers {
		id := strings.TrimSpace(trigger.TriggerID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		trigger.TriggerID = id
		cleaned = append(cleaned, trigger)
	}
	return cleaned
}

// awsFreshnessFencedTriggerPlaceholders returns one "($n, $n+1)" pair per
// row for the VALUES(trigger_id, fencing_token) clause the mark-handed-off/
// failed query formats join against (#4576).
func awsFreshnessFencedTriggerPlaceholders(rowCount int) string {
	pairs := make([]string, rowCount)
	for i := range pairs {
		idParam := i*2 + 1
		tokenParam := i*2 + 2
		pairs[i] = fmt.Sprintf("($%d, $%d::bigint)", idParam, tokenParam)
	}
	return strings.Join(pairs, ", ")
}

// awsFreshnessFencedTriggerArgs interleaves each trigger's id and fencing
// token (matching awsFreshnessFencedTriggerPlaceholders's pairing) ahead of
// any trailing args (failure class/message/timestamp).
func awsFreshnessFencedTriggerArgs(triggers []freshness.StoredTrigger, extra ...any) []any {
	args := make([]any, 0, len(triggers)*2+len(extra))
	for _, trigger := range triggers {
		args = append(args, trigger.TriggerID, trigger.ClaimFencingToken)
	}
	return append(args, extra...)
}
