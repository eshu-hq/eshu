// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// IncidentFreshnessStore persists incident-source webhook refresh triggers for
// later workflow coordinator handoff.
type IncidentFreshnessStore struct {
	db ExecQueryer
}

// NewIncidentFreshnessStore constructs a Postgres-backed incident freshness
// trigger store.
func NewIncidentFreshnessStore(db ExecQueryer) *IncidentFreshnessStore {
	return &IncidentFreshnessStore{db: db}
}

// IncidentFreshnessSchemaSQL returns the DDL for incident freshness triggers.
func IncidentFreshnessSchemaSQL() string {
	return incidentFreshnessSchemaSQL
}

// EnsureSchema applies the incident freshness trigger schema.
func (s *IncidentFreshnessStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("incident freshness store database is required")
	}
	if _, err := s.db.ExecContext(ctx, incidentFreshnessSchemaSQL); err != nil {
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
	if s.db == nil {
		return webhook.StoredIncidentFreshnessTrigger{}, errors.New("incident freshness store database is required")
	}
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	rows, err := s.db.QueryContext(
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

// ClaimQueuedTriggers marks queued incident freshness triggers as claimed for
// one workflow handoff actor.
func (s *IncidentFreshnessStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
) ([]webhook.StoredIncidentFreshnessTrigger, error) {
	if s.db == nil {
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
	rows, err := s.db.QueryContext(ctx, claimQueuedIncidentFreshnessTriggersQuery, limit, owner, claimedAt.UTC())
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

// MarkTriggersHandedOff records successful workflow handoff for claimed
// incident freshness triggers.
func (s *IncidentFreshnessStore) MarkTriggersHandedOff(ctx context.Context, triggerIDs []string, handedOffAt time.Time) error {
	if s.db == nil {
		return errors.New("incident freshness store database is required")
	}
	cleaned := cleanTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("incident freshness trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("incident freshness handed_off_at is required")
	}
	args := triggerIDArgs(cleaned, handedOffAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkIncidentFreshnessTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark incident freshness triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records failed workflow handoff for claimed incident
// freshness triggers.
func (s *IncidentFreshnessStore) MarkTriggersFailed(
	ctx context.Context,
	triggerIDs []string,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.db == nil {
		return errors.New("incident freshness store database is required")
	}
	cleaned := cleanTriggerIDs(triggerIDs)
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
	args := triggerIDArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkIncidentFreshnessTriggersFailedQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark incident freshness triggers failed: %w", err)
	}
	return nil
}

func scanIncidentFreshnessTrigger(rows Rows) (webhook.StoredIncidentFreshnessTrigger, error) {
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
	); err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	stored.Provider = webhook.Provider(provider)
	stored.Status = webhook.TriggerStatus(status)
	return stored, nil
}

func buildMarkIncidentFreshnessTriggersHandedOffQuery(idCount int) string {
	timestampParam := idCount + 1
	return fmt.Sprintf(markIncidentFreshnessTriggersHandedOffQueryFormat, timestampParam, timestampParam, triggerIDPlaceholders(idCount))
}

func buildMarkIncidentFreshnessTriggersFailedQuery(idCount int) string {
	failureClassParam := idCount + 1
	failureMessageParam := idCount + 2
	timestampParam := idCount + 3
	return fmt.Sprintf(
		markIncidentFreshnessTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		triggerIDPlaceholders(idCount),
	)
}

func incidentFreshnessBootstrapDefinition() Definition {
	return Definition{
		Name: "incident_freshness_triggers",
		Path: "schema/data-plane/postgres/023_incident_freshness_triggers.sql",
		SQL:  incidentFreshnessSchemaSQL,
	}
}
