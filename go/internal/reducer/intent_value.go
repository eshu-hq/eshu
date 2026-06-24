// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Intent describes one durable reducer follow-up action keyed by scope
// generation.
type Intent struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Domain          Domain
	Cause           string
	Priority        int
	AttemptCount    int
	EntityKeys      []string
	RelatedScopeIDs []string
	Payload         map[string]any
	Status          IntentStatus
	EnqueuedAt      time.Time
	AvailableAt     time.Time
	ClaimedAt       *time.Time
	CompletedAt     *time.Time
	Failure         *FailureRecord
}

// ScopeGenerationKey returns the durable scope-generation boundary for the intent.
func (i Intent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

// Validate checks the durable intent contract.
func (i Intent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" {
		return errors.New("intent_id must not be blank")
	}
	if strings.TrimSpace(i.ScopeID) == "" {
		return errors.New("scope_id must not be blank")
	}
	if strings.TrimSpace(i.GenerationID) == "" {
		return errors.New("generation_id must not be blank")
	}
	if strings.TrimSpace(i.SourceSystem) == "" {
		return errors.New("source_system must not be blank")
	}
	if err := i.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(i.Cause) == "" {
		return errors.New("cause must not be blank")
	}
	if i.EnqueuedAt.IsZero() {
		return errors.New("enqueued_at must not be zero")
	}
	if i.AvailableAt.IsZero() {
		return errors.New("available_at must not be zero")
	}
	if len(i.RelatedScopeIDs) == 0 {
		return errors.New("related_scope_ids must not be empty")
	}
	if err := i.Status.Validate(); err != nil {
		return err
	}

	for _, key := range i.EntityKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("entity_keys must not contain blank values")
		}
	}
	var seenRelatedScopes map[string]struct{}
	for _, scopeID := range i.RelatedScopeIDs {
		normalizedScopeID := strings.TrimSpace(scopeID)
		if normalizedScopeID == "" {
			return errors.New("related_scope_ids must not contain blank values")
		}
		if seenRelatedScopes == nil {
			seenRelatedScopes = make(map[string]struct{}, len(i.RelatedScopeIDs))
		}
		if _, exists := seenRelatedScopes[normalizedScopeID]; exists {
			return errors.New("related_scope_ids must not contain duplicate values")
		}
		seenRelatedScopes[normalizedScopeID] = struct{}{}
	}

	return nil
}

// Clone returns a replay-safe copy of the intent.
func (i Intent) Clone() Intent {
	cloned := i
	cloned.EntityKeys = slices.Clone(i.EntityKeys)
	cloned.RelatedScopeIDs = slices.Clone(i.RelatedScopeIDs)
	if i.Payload != nil {
		cloned.Payload = make(map[string]any, len(i.Payload))
		for key, value := range i.Payload {
			cloned.Payload[key] = value
		}
	}
	if i.ClaimedAt != nil {
		claimedAt := *i.ClaimedAt
		cloned.ClaimedAt = &claimedAt
	}
	if i.CompletedAt != nil {
		completedAt := *i.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	if i.Failure != nil {
		failure := *i.Failure
		cloned.Failure = &failure
	}

	return cloned
}

// Validate checks that the lifecycle state is one of the known durable values.
func (status IntentStatus) Validate() error {
	switch status {
	case IntentStatusPending, IntentStatusClaimed, IntentStatusRunning, IntentStatusSucceeded, IntentStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown intent status %q", status)
	}
}

// Terminal reports whether the status represents a final state.
func (status IntentStatus) Terminal() bool {
	switch status {
	case IntentStatusSucceeded, IntentStatusFailed:
		return true
	default:
		return false
	}
}

// WithStatus returns a clone of the intent with the given status and timestamp.
func (i Intent) WithStatus(status IntentStatus, at time.Time) Intent {
	cloned := i.Clone()
	cloned.Status = status
	switch status {
	case IntentStatusClaimed:
		cloned.ClaimedAt = &at
	case IntentStatusRunning:
		cloned.ClaimedAt = &at
	case IntentStatusSucceeded, IntentStatusFailed:
		cloned.CompletedAt = &at
	}

	return cloned
}
