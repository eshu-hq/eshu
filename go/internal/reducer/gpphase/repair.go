// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PhaseRepair captures one exact readiness publication that must be retried
// after the underlying graph write already committed successfully.
type PhaseRepair struct {
	Key           PhaseKey
	Phase         Phase
	CommittedAt   time.Time
	EnqueuedAt    time.Time
	NextAttemptAt time.Time
	UpdatedAt     time.Time
	Attempts      int
	LastError     string
}

// Validate checks that the repair row is specific enough to replay safely.
func (r PhaseRepair) Validate() error {
	if err := r.Key.Validate(); err != nil {
		return fmt.Errorf("validate repair key: %w", err)
	}
	if strings.TrimSpace(string(r.Phase)) == "" {
		return fmt.Errorf("phase must not be blank")
	}
	return nil
}

// PhaseRepairQueue persists exact readiness publications that must be
// retried later after a publish failure.
type PhaseRepairQueue interface {
	Enqueue(context.Context, []PhaseRepair) error
	ListDue(context.Context, time.Time, int) ([]PhaseRepair, error)
	Delete(context.Context, []PhaseRepair) error
	MarkFailed(context.Context, PhaseRepair, time.Time, string) error
}

// PhaseRepairsFromStates converts exact readiness publications into durable
// repair rows that can be retried later if publication failed.
func PhaseRepairsFromStates(
	states []PhaseState,
	lastError string,
	enqueuedAt time.Time,
) []PhaseRepair {
	repairs := make([]PhaseRepair, 0, len(states))
	queuedAt := enqueuedAt.UTC()
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	for _, state := range states {
		repairs = append(repairs, PhaseRepair{
			Key:           state.Key,
			Phase:         state.Phase,
			CommittedAt:   state.CommittedAt,
			EnqueuedAt:    queuedAt,
			NextAttemptAt: queuedAt,
			UpdatedAt:     queuedAt,
			LastError:     lastError,
		})
	}
	return repairs
}
