// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// This file holds the in-memory coalescing stores behind the #9377
// extracted-path parity harness (intake_parity_cases_test.go). Each fake
// implements its intake Stores interface with the same coalescing identity
// the Postgres upserts enforce (refresh key, freshness key, delivery key),
// so the harness proves wiring, auth, status codes, dedup convergence, and
// claim ordering through listener → intake API → store. The real SQL
// semantics are proven separately against live Postgres.

// parityRepositoryStore coalesces repository triggers by refresh key
// (provider|repository|branch|sha) with the ignored-to-queued promotion the
// webhook_refresh_triggers upsert applies.
type parityRepositoryStore struct {
	mu      sync.Mutex
	failErr error
	seq     int
	rows    map[string]*webhook.StoredTrigger
}

func (s *parityRepositoryStore) StoreTrigger(_ context.Context, trigger webhook.Trigger, receivedAt time.Time) (webhook.StoredTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failErr != nil {
		return webhook.StoredTrigger{}, s.failErr
	}
	if s.rows == nil {
		s.rows = map[string]*webhook.StoredTrigger{}
	}
	key := string(trigger.Provider) + "|" + trigger.RepositoryExternalID + "|" + trigger.DefaultBranch + "|" + trigger.TargetSHA
	if existing, ok := s.rows[key]; ok {
		existing.DuplicateCount++
		existing.DeliveryID = trigger.DeliveryID
		if existing.Status == webhook.TriggerStatusIgnored && trigger.Decision == webhook.DecisionAccepted {
			existing.Status = webhook.TriggerStatusQueued
		}
		if existing.Status == webhook.TriggerStatusQueued && trigger.Decision == webhook.DecisionIgnored {
			existing.Status = webhook.TriggerStatusIgnored
		}
		return *existing, nil
	}
	s.seq++
	status := webhook.TriggerStatusQueued
	if trigger.Decision == webhook.DecisionIgnored {
		status = webhook.TriggerStatusIgnored
	}
	stored := &webhook.StoredTrigger{
		Trigger:     trigger,
		TriggerID:   fmt.Sprintf("parity-repo-%04d", s.seq),
		DeliveryKey: string(trigger.Provider) + ":" + trigger.DeliveryID,
		RefreshKey:  key,
		Status:      status,
		ReceivedAt:  receivedAt,
		UpdatedAt:   receivedAt,
	}
	s.rows[key] = stored
	return *stored, nil
}

// claim returns up to limit queued rows in (received_at, trigger_id) order,
// marking them claimed. It mirrors the webhook store's claim order so the
// harness proves FIFO convergence through the API path.
func (s *parityRepositoryStore) claim(limit int) []webhook.StoredTrigger {
	s.mu.Lock()
	defer s.mu.Unlock()
	queued := make([]*webhook.StoredTrigger, 0)
	for _, row := range s.rows {
		if row.Status == webhook.TriggerStatusQueued {
			queued = append(queued, row)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].ReceivedAt.Equal(queued[j].ReceivedAt) {
			return queued[i].TriggerID < queued[j].TriggerID
		}
		return queued[i].ReceivedAt.Before(queued[j].ReceivedAt)
	})
	out := make([]webhook.StoredTrigger, 0, limit)
	for _, row := range queued {
		if len(out) >= limit {
			break
		}
		row.Status = webhook.TriggerStatusClaimed
		out = append(out, *row)
	}
	return out
}

func (s *parityRepositoryStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func (s *parityRepositoryStore) duplicateCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.rows[key]; ok {
		return row.DuplicateCount
	}
	return -1
}

// parityIncidentStore coalesces incident freshness triggers by freshness
// key (provider|scope), preserving claimed rows across redelivery like the
// incident_freshness_triggers upsert.
type parityIncidentStore struct {
	mu   sync.Mutex
	rows map[string]*webhook.StoredIncidentFreshnessTrigger
}

func (s *parityIncidentStore) StoreIncidentFreshnessTrigger(_ context.Context, trigger webhook.IncidentFreshnessTrigger, receivedAt time.Time) (webhook.StoredIncidentFreshnessTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = map[string]*webhook.StoredIncidentFreshnessTrigger{}
	}
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	if existing, ok := s.rows[stored.FreshnessKey]; ok {
		existing.DuplicateCount++
		existing.UpdatedAt = receivedAt
		if existing.Status != webhook.TriggerStatusClaimed {
			existing.DeliveryKey = stored.DeliveryKey
			existing.EventID = trigger.EventID
			existing.Status = stored.Status
		}
		return *existing, nil
	}
	s.rows[stored.FreshnessKey] = &stored
	return stored, nil
}

func (s *parityIncidentStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// parityAWSStore coalesces AWS freshness triggers by freshness key,
// preserving claimed rows across redelivery.
type parityAWSStore struct {
	mu   sync.Mutex
	rows map[string]*awsfreshness.StoredTrigger
}

func (s *parityAWSStore) StoreTrigger(_ context.Context, trigger awsfreshness.Trigger, receivedAt time.Time) (awsfreshness.StoredTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := trigger.Validate(); err != nil {
		return awsfreshness.StoredTrigger{}, err
	}
	if s.rows == nil {
		s.rows = map[string]*awsfreshness.StoredTrigger{}
	}
	key := trigger.AccountID + "|" + trigger.Region + "|" + trigger.ServiceKind
	if existing, ok := s.rows[key]; ok {
		existing.DuplicateCount++
		existing.UpdatedAt = receivedAt
		if existing.Status != awsfreshness.TriggerStatusClaimed {
			existing.EventID = trigger.EventID
			existing.Status = awsfreshness.TriggerStatusQueued
		}
		return *existing, nil
	}
	stored := &awsfreshness.StoredTrigger{
		Trigger:      trigger,
		TriggerID:    "parity-aws-" + key,
		DeliveryKey:  "aws:" + trigger.EventID,
		FreshnessKey: key,
		Status:       awsfreshness.TriggerStatusQueued,
		ReceivedAt:   receivedAt,
		UpdatedAt:    receivedAt,
	}
	s.rows[key] = stored
	return *stored, nil
}

func (s *parityAWSStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// parityGCPStore coalesces GCP freshness triggers by freshness key,
// preserving claimed rows across redelivery.
type parityGCPStore struct {
	mu   sync.Mutex
	rows map[string]*gcpfreshness.StoredTrigger
}

func (s *parityGCPStore) StoreTrigger(_ context.Context, trigger gcpfreshness.Trigger, receivedAt time.Time) (gcpfreshness.StoredTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := trigger.Validate(); err != nil {
		return gcpfreshness.StoredTrigger{}, err
	}
	if s.rows == nil {
		s.rows = map[string]*gcpfreshness.StoredTrigger{}
	}
	key := string(trigger.ParentScopeKind) + "|" + trigger.ParentScopeID + "|" + trigger.AssetType
	if existing, ok := s.rows[key]; ok {
		existing.DuplicateCount++
		existing.UpdatedAt = receivedAt
		if existing.Status != catalog.TriggerStatusClaimed {
			existing.EventID = trigger.EventID
			existing.Status = catalog.TriggerStatusQueued
		}
		return *existing, nil
	}
	stored := &gcpfreshness.StoredTrigger{
		Trigger:      trigger,
		TriggerID:    "parity-gcp-" + key,
		DeliveryKey:  "gcp:" + trigger.EventID,
		FreshnessKey: key,
		Status:       catalog.TriggerStatusQueued,
		ReceivedAt:   receivedAt,
		UpdatedAt:    receivedAt,
	}
	s.rows[key] = stored
	return *stored, nil
}

func (s *parityGCPStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

// parityActionsStore dedupes Actions deliveries by delivery ID: an exact
// identity replay returns the stored row (the listener answers 202), while
// a conflicting identity for the same delivery fails with
// ErrActionsDeliveryConflict (the listener answers 409).
type parityActionsStore struct {
	mu   sync.Mutex
	rows map[string]webhook.StoredActionsTrigger
}

func (s *parityActionsStore) Store(_ context.Context, trigger webhook.ActionsTrigger, receivedAt time.Time) (webhook.StoredActionsTrigger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = map[string]webhook.StoredActionsTrigger{}
	}
	if existing, ok := s.rows[trigger.DeliveryID]; ok {
		if existing.ActionsTrigger != trigger {
			return webhook.StoredActionsTrigger{}, webhook.ErrActionsDeliveryConflict
		}
		return existing, nil
	}
	stored := webhook.StoredActionsTrigger{
		ActionsTrigger: trigger,
		Status:         webhook.TriggerStatusQueued,
		ReceivedAt:     receivedAt,
		UpdatedAt:      receivedAt,
	}
	s.rows[trigger.DeliveryID] = stored
	return stored, nil
}

func (s *parityActionsStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}
