package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GraphProjectionPhaseRepair captures one exact readiness publication that must
// be retried after the underlying graph write already committed successfully.
type GraphProjectionPhaseRepair struct {
	Key           GraphProjectionPhaseKey
	Phase         GraphProjectionPhase
	CommittedAt   time.Time
	EnqueuedAt    time.Time
	NextAttemptAt time.Time
	UpdatedAt     time.Time
	Attempts      int
	LastError     string
}

// Validate checks that the repair row is specific enough to replay safely.
func (r GraphProjectionPhaseRepair) Validate() error {
	if err := r.Key.Validate(); err != nil {
		return fmt.Errorf("validate repair key: %w", err)
	}
	if strings.TrimSpace(string(r.Phase)) == "" {
		return fmt.Errorf("phase must not be blank")
	}
	return nil
}

// GraphProjectionPhaseRepairQueue persists exact readiness publications that
// must be retried later after a publish failure.
type GraphProjectionPhaseRepairQueue interface {
	Enqueue(context.Context, []GraphProjectionPhaseRepair) error
	ListDue(context.Context, time.Time, int) ([]GraphProjectionPhaseRepair, error)
	Delete(context.Context, []GraphProjectionPhaseRepair) error
	MarkFailed(context.Context, GraphProjectionPhaseRepair, time.Time, string) error
}

// GraphProjectionPhaseRepairsFromStates converts exact readiness publications
// into durable repair rows that can be retried later if publication failed.
func GraphProjectionPhaseRepairsFromStates(
	states []GraphProjectionPhaseState,
	lastError string,
	enqueuedAt time.Time,
) []GraphProjectionPhaseRepair {
	repairs := make([]GraphProjectionPhaseRepair, 0, len(states))
	queuedAt := enqueuedAt.UTC()
	if queuedAt.IsZero() {
		queuedAt = time.Now().UTC()
	}

	for _, state := range states {
		repairs = append(repairs, GraphProjectionPhaseRepair{
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
