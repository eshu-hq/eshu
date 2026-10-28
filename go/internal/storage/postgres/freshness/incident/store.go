// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package incidentfreshnessstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// IncidentFreshnessStore persists incident-source webhook refresh triggers for
// later workflow coordinator handoff.
type IncidentFreshnessStore struct {
	database db.ExecQueryer
}

// NewIncidentFreshnessStore constructs a Postgres-backed incident freshness
// trigger store.
func NewIncidentFreshnessStore(database db.ExecQueryer) *IncidentFreshnessStore {
	return &IncidentFreshnessStore{database: database}
}

// IncidentFreshnessSchemaSQL returns the DDL for incident freshness triggers.
func IncidentFreshnessSchemaSQL() string {
	return incidentFreshnessSchemaSQL
}

// EnsureSchema applies the incident freshness trigger schema.
func (s *IncidentFreshnessStore) EnsureSchema(ctx context.Context) error {
	if s.database == nil {
		return errors.New("incident freshness store database is required")
	}
	if _, err := s.database.ExecContext(ctx, incidentFreshnessSchemaSQL); err != nil {
		return fmt.Errorf("ensure incident freshness schema: %w", err)
	}
	return nil
}

// StoreIncidentFreshnessTrigger persists and coalesces one verified incident
// source refresh trigger.
func (s *IncidentFreshnessStore) StoreIncidentFreshnessTrigger(
	ctx context.Context,
	trigger webhook.IncidentFreshnessTrigger,
	receivedAt time.Time,
) (webhook.StoredIncidentFreshnessTrigger, error) {
	if s.database == nil {
		return webhook.StoredIncidentFreshnessTrigger{}, errors.New("incident freshness store database is required")
	}
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	rows, err := s.database.QueryContext(
		ctx,
		storeIncidentFreshnessTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(stored.Provider),
		stored.EventKind,
		stored.EventID,
		stored.ScopeID,
		stored.ResourceID,
		string(stored.Status),
		stored.ObservedAt,
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, fmt.Errorf("store incident freshness trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return webhook.StoredIncidentFreshnessTrigger{}, fmt.Errorf("store incident freshness trigger: %w", err)
		}
		return webhook.StoredIncidentFreshnessTrigger{}, errors.New("store incident freshness trigger returned no row")
	}
	stored, err = scanIncidentFreshnessTrigger(rows)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, fmt.Errorf("store incident freshness trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, fmt.Errorf("store incident freshness trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for one handoff actor.
// The claim carries a claim_expires_at lease (claimedAt+leaseDuration) so a
// mid-batch handoff abort or coordinator crash cannot strand the row at
// 'claimed' forever; a later ReapExpiredTriggerClaims call requeues it once
// the lease expires (#9379).
func (s *IncidentFreshnessStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
	leaseDuration time.Duration,
) ([]webhook.StoredIncidentFreshnessTrigger, error) {
	if s.database == nil {
		return nil, errors.New("incident freshness store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("incident freshness claim owner is required")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("incident freshness claimed_at is required")
	}
	if limit <= 0 {
		return nil, errors.New("incident freshness claim limit must be positive")
	}
	if leaseDuration <= 0 {
		return nil, errors.New("incident freshness claim lease duration must be positive")
	}
	claimedAtUTC := claimedAt.UTC()
	rows, err := s.database.QueryContext(
		ctx,
		claimQueuedIncidentFreshnessTriggersQuery,
		limit,
		owner,
		claimedAtUTC,
		claimedAtUTC.Add(leaseDuration),
	)
	if err != nil {
		return nil, fmt.Errorf("claim incident freshness triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]webhook.StoredIncidentFreshnessTrigger, 0)
	for rows.Next() {
		trigger, err := scanIncidentFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim incident freshness triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim incident freshness triggers: %w", err)
	}
	return triggers, nil
}

// ReapExpiredTriggerClaims reclaims 'claimed' incident freshness triggers
// whose claim lease has expired back to 'queued', mirroring the
// workflow_claims expired-lease reclaim pattern (#4464) and the AWS/GCP
// freshness reap (#4576). This is what recovers a trigger stranded at
// 'claimed' by a mid-batch handoff abort or a coordinator crash between
// claim and mark-handed-off/failed (#9379).
func (s *IncidentFreshnessStore) ReapExpiredTriggerClaims(
	ctx context.Context,
	asOf time.Time,
	limit int,
) ([]webhook.StoredIncidentFreshnessTrigger, error) {
	if s.database == nil {
		return nil, errors.New("incident freshness store database is required")
	}
	if asOf.IsZero() {
		return nil, errors.New("incident freshness reap as-of time is required")
	}
	if limit <= 0 {
		return nil, errors.New("incident freshness reap limit must be positive")
	}
	rows, err := s.database.QueryContext(ctx, reapExpiredIncidentFreshnessTriggerClaimsQuery, asOf.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("reap expired incident freshness trigger claims: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]webhook.StoredIncidentFreshnessTrigger, 0)
	for rows.Next() {
		trigger, err := scanIncidentFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("reap expired incident freshness trigger claims: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reap expired incident freshness trigger claims: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records successful workflow handoff for claimed
// triggers. Each write is fenced by the trigger's ClaimFencingToken (#9379):
// a row completes only if its current claim_fencing_token still matches the
// token the caller received from ClaimQueuedTriggers, so a stale claimant
// whose lease expired and was reaped by ReapExpiredTriggerClaims — and whose
// trigger a different owner then re-claimed, bumping the token again —
// cannot complete a claim it no longer holds. It is not an error for fewer
// rows to be affected than triggers passed in: a fenced-out row is exactly
// the case this guards against, not a failure worth surfacing as one (the
// stale caller already lost the race and has nothing further to do).
func (s *IncidentFreshnessStore) MarkTriggersHandedOff(ctx context.Context, triggers []webhook.StoredIncidentFreshnessTrigger, handedOffAt time.Time) error {
	if s.database == nil {
		return errors.New("incident freshness store database is required")
	}
	cleaned := cleanIncidentFreshnessTriggerClaims(triggers)
	if len(cleaned) == 0 {
		return errors.New("incident freshness trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("incident freshness handed_off_at is required")
	}
	args := incidentFreshnessFencedTriggerArgs(cleaned, handedOffAt.UTC())
	if _, err := s.database.ExecContext(ctx, buildMarkIncidentFreshnessTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark incident freshness triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records failed workflow handoff for claimed triggers.
// See MarkTriggersHandedOff's doc comment for the claim_fencing_token
// fencing rationale (#9379).
func (s *IncidentFreshnessStore) MarkTriggersFailed(
	ctx context.Context,
	triggers []webhook.StoredIncidentFreshnessTrigger,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.database == nil {
		return errors.New("incident freshness store database is required")
	}
	cleaned := cleanIncidentFreshnessTriggerClaims(triggers)
	if len(cleaned) == 0 {
		return errors.New("incident freshness trigger ids are required")
	}
	if failedAt.IsZero() {
		return errors.New("incident freshness failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("incident freshness failure class is required")
	}
	args := incidentFreshnessFencedTriggerArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.database.ExecContext(ctx, buildMarkIncidentFreshnessTriggersFailedQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark incident freshness triggers failed: %w", err)
	}
	return nil
}

// scanIncidentFreshnessTrigger scans a row shape ending in
// claim_fencing_token. Callers that RETURNING a row without that trailing
// column (none currently) must not use this scanner.
func scanIncidentFreshnessTrigger(rows db.Rows) (webhook.StoredIncidentFreshnessTrigger, error) {
	var stored webhook.StoredIncidentFreshnessTrigger
	var provider, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.FreshnessKey,
		&provider,
		&stored.EventKind,
		&stored.EventID,
		&stored.ScopeID,
		&stored.ResourceID,
		&status,
		&stored.DuplicateCount,
		&stored.ObservedAt,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
		&stored.ClaimFencingToken,
	); err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	stored.Provider = webhook.Provider(provider)
	stored.Status = webhook.TriggerStatus(status)
	return stored, nil
}

func buildMarkIncidentFreshnessTriggersHandedOffQuery(rowCount int) string {
	timestampParam := rowCount*2 + 1
	return fmt.Sprintf(markIncidentFreshnessTriggersHandedOffQueryFormat, timestampParam, timestampParam, incidentFreshnessFencedTriggerPlaceholders(rowCount))
}

func buildMarkIncidentFreshnessTriggersFailedQuery(rowCount int) string {
	failureClassParam := rowCount*2 + 1
	failureMessageParam := rowCount*2 + 2
	timestampParam := rowCount*2 + 3
	return fmt.Sprintf(
		markIncidentFreshnessTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		incidentFreshnessFencedTriggerPlaceholders(rowCount),
	)
}

// cleanIncidentFreshnessTriggerClaims dedupes triggers by trigger_id (keeping
// the first ClaimFencingToken seen for that id) and drops blank-id rows. A
// trigger with a blank id cannot be fenced (there is nothing to match on),
// so it is dropped rather than sent to the database.
func cleanIncidentFreshnessTriggerClaims(triggers []webhook.StoredIncidentFreshnessTrigger) []webhook.StoredIncidentFreshnessTrigger {
	cleaned := make([]webhook.StoredIncidentFreshnessTrigger, 0, len(triggers))
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

// incidentFreshnessFencedTriggerPlaceholders returns one "($n, $n+1)" pair
// per row for the VALUES(trigger_id, fencing_token) clause the
// mark-handed-off/failed query formats join against (#9379).
func incidentFreshnessFencedTriggerPlaceholders(rowCount int) string {
	pairs := make([]string, rowCount)
	for i := range pairs {
		idParam := i*2 + 1
		tokenParam := i*2 + 2
		pairs[i] = fmt.Sprintf("($%d, $%d::bigint)", idParam, tokenParam)
	}
	return strings.Join(pairs, ", ")
}

// incidentFreshnessFencedTriggerArgs interleaves each trigger's id and
// fencing token (matching incidentFreshnessFencedTriggerPlaceholders's
// pairing) ahead of any trailing args (failure class/message/timestamp).
func incidentFreshnessFencedTriggerArgs(triggers []webhook.StoredIncidentFreshnessTrigger, extra ...any) []any {
	args := make([]any, 0, len(triggers)*2+len(extra))
	for _, trigger := range triggers {
		args = append(args, trigger.TriggerID, trigger.ClaimFencingToken)
	}
	return append(args, extra...)
}
