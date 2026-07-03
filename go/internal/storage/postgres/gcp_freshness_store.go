// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
)

// GCPFreshnessStore persists GCP Cloud Asset Inventory event-driven refresh
// triggers for later workflow handoff.
type GCPFreshnessStore struct {
	db ExecQueryer
}

// NewGCPFreshnessStore constructs a Postgres-backed GCP freshness store.
func NewGCPFreshnessStore(db ExecQueryer) *GCPFreshnessStore {
	return &GCPFreshnessStore{db: db}
}

// GCPFreshnessSchemaSQL returns the DDL for the GCP freshness trigger store.
func GCPFreshnessSchemaSQL() string {
	return gcpFreshnessSchemaSQL
}

// EnsureSchema applies the GCP freshness trigger schema.
func (s *GCPFreshnessStore) EnsureSchema(ctx context.Context) error {
	if s.db == nil {
		return errors.New("GCP freshness store database is required")
	}
	if _, err := s.db.ExecContext(ctx, gcpFreshnessSchemaSQL); err != nil {
		return fmt.Errorf("ensure GCP freshness schema: %w", err)
	}
	return nil
}

// StoreTrigger persists and coalesces one normalized GCP freshness event.
func (s *GCPFreshnessStore) StoreTrigger(
	ctx context.Context,
	trigger freshness.Trigger,
	receivedAt time.Time,
) (freshness.StoredTrigger, error) {
	if s.db == nil {
		return freshness.StoredTrigger{}, errors.New("GCP freshness store database is required")
	}
	stored, err := freshness.NewStoredTrigger(trigger, receivedAt)
	if err != nil {
		return freshness.StoredTrigger{}, err
	}
	rows, err := s.db.QueryContext(
		ctx,
		storeGCPFreshnessTriggerQuery,
		stored.TriggerID,
		stored.DeliveryKey,
		stored.FreshnessKey,
		string(stored.Kind),
		stored.EventID,
		string(stored.ParentScopeKind),
		stored.ParentScopeID,
		stored.AssetType,
		stored.Location,
		string(stored.Status),
		stored.ObservedAt,
		stored.ReceivedAt,
		stored.UpdatedAt,
	)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store GCP freshness trigger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return freshness.StoredTrigger{}, fmt.Errorf("store GCP freshness trigger: %w", err)
		}
		return freshness.StoredTrigger{}, errors.New("store GCP freshness trigger returned no row")
	}
	stored, err = scanGCPFreshnessTrigger(rows)
	if err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store GCP freshness trigger: %w", err)
	}
	if err := rows.Err(); err != nil {
		return freshness.StoredTrigger{}, fmt.Errorf("store GCP freshness trigger: %w", err)
	}
	return stored, nil
}

// ClaimQueuedTriggers marks queued triggers as claimed for one handoff actor.
func (s *GCPFreshnessStore) ClaimQueuedTriggers(
	ctx context.Context,
	owner string,
	claimedAt time.Time,
	limit int,
) ([]freshness.StoredTrigger, error) {
	if s.db == nil {
		return nil, errors.New("GCP freshness store database is required")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, errors.New("GCP freshness claim owner is required")
	}
	if claimedAt.IsZero() {
		return nil, errors.New("GCP freshness claimed_at is required")
	}
	if limit <= 0 {
		return nil, errors.New("GCP freshness claim limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, claimQueuedGCPFreshnessTriggersQuery, limit, owner, claimedAt.UTC())
	if err != nil {
		return nil, fmt.Errorf("claim GCP freshness triggers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	triggers := make([]freshness.StoredTrigger, 0)
	for rows.Next() {
		trigger, err := scanGCPFreshnessTrigger(rows)
		if err != nil {
			return nil, fmt.Errorf("claim GCP freshness triggers: %w", err)
		}
		triggers = append(triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim GCP freshness triggers: %w", err)
	}
	return triggers, nil
}

// MarkTriggersHandedOff records successful workflow handoff for claimed
// triggers.
func (s *GCPFreshnessStore) MarkTriggersHandedOff(ctx context.Context, triggerIDs []string, handedOffAt time.Time) error {
	if s.db == nil {
		return errors.New("GCP freshness store database is required")
	}
	cleaned := cleanGCPFreshnessTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("GCP freshness trigger ids are required")
	}
	if handedOffAt.IsZero() {
		return errors.New("GCP freshness handed_off_at is required")
	}
	args := gcpFreshnessTriggerIDArgs(cleaned, handedOffAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkGCPFreshnessTriggersHandedOffQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark GCP freshness triggers handed off: %w", err)
	}
	return nil
}

// MarkTriggersFailed records failed workflow handoff for claimed triggers.
func (s *GCPFreshnessStore) MarkTriggersFailed(
	ctx context.Context,
	triggerIDs []string,
	failedAt time.Time,
	failureClass string,
	failureMessage string,
) error {
	if s.db == nil {
		return errors.New("GCP freshness store database is required")
	}
	cleaned := cleanGCPFreshnessTriggerIDs(triggerIDs)
	if len(cleaned) == 0 {
		return errors.New("GCP freshness trigger ids are required")
	}
	if failedAt.IsZero() {
		return errors.New("GCP freshness failed_at is required")
	}
	failureClass = strings.TrimSpace(failureClass)
	if failureClass == "" {
		return errors.New("GCP freshness failure class is required")
	}
	args := gcpFreshnessTriggerIDArgs(cleaned, failureClass, strings.TrimSpace(failureMessage), failedAt.UTC())
	if _, err := s.db.ExecContext(ctx, buildMarkGCPFreshnessTriggersFailedQuery(len(cleaned)), args...); err != nil {
		return fmt.Errorf("mark GCP freshness triggers failed: %w", err)
	}
	return nil
}

func scanGCPFreshnessTrigger(rows Rows) (freshness.StoredTrigger, error) {
	var stored freshness.StoredTrigger
	var kind, parentScopeKind, status string
	if err := rows.Scan(
		&stored.TriggerID,
		&stored.DeliveryKey,
		&stored.FreshnessKey,
		&kind,
		&stored.EventID,
		&parentScopeKind,
		&stored.ParentScopeID,
		&stored.AssetType,
		&stored.Location,
		&status,
		&stored.DuplicateCount,
		&stored.ObservedAt,
		&stored.ReceivedAt,
		&stored.UpdatedAt,
	); err != nil {
		return freshness.StoredTrigger{}, err
	}
	stored.Kind = freshness.EventKind(kind)
	stored.ParentScopeKind = gcpcloud.ParentScopeKind(parentScopeKind)
	stored.Status = freshness.TriggerStatus(status)
	return stored, nil
}

func buildMarkGCPFreshnessTriggersHandedOffQuery(idCount int) string {
	timestampParam := idCount + 1
	return fmt.Sprintf(markGCPFreshnessTriggersHandedOffQueryFormat, timestampParam, timestampParam, gcpFreshnessTriggerIDPlaceholders(idCount))
}

func buildMarkGCPFreshnessTriggersFailedQuery(idCount int) string {
	failureClassParam := idCount + 1
	failureMessageParam := idCount + 2
	timestampParam := idCount + 3
	return fmt.Sprintf(
		markGCPFreshnessTriggersFailedQueryFormat,
		failureClassParam,
		failureMessageParam,
		timestampParam,
		timestampParam,
		gcpFreshnessTriggerIDPlaceholders(idCount),
	)
}

func cleanGCPFreshnessTriggerIDs(ids []string) []string {
	cleaned := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	return cleaned
}

func gcpFreshnessTriggerIDPlaceholders(count int) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	return strings.Join(placeholders, ", ")
}

func gcpFreshnessTriggerIDArgs(ids []string, extra ...any) []any {
	args := make([]any, 0, len(ids)+len(extra))
	for _, id := range ids {
		args = append(args, id)
	}
	return append(args, extra...)
}
