// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/failure"
)

// conflictingWorkSource reports a transient claim conflict for the first
// conflicts calls and then an empty queue.
type conflictingWorkSource struct {
	mu        sync.Mutex
	conflicts int
	calls     int
}

func (s *conflictingWorkSource) Claim(context.Context) (ScopeGenerationWork, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls <= s.conflicts {
		return ScopeGenerationWork{}, false, fmt.Errorf("claim projector work: %w: deadlock detected (SQLSTATE 40P01)",
			failure.ErrWorkClaimConflict)
	}
	return ScopeGenerationWork{}, false, nil
}

func (s *conflictingWorkSource) drained() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls > s.conflicts
}

func TestServiceRunKeepsWorkersAfterClaimConflict(t *testing.T) {
	t.Parallel()

	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers_%d", workers), func(t *testing.T) {
			t.Parallel()
			source := &conflictingWorkSource{conflicts: 2 * workers}
			service := Service{
				Workers:    workers,
				WorkSource: source,
				FactStore:  &stubFactStore{},
				Runner:     &stubProjectionRunner{},
				WorkSink:   &stubProjectorWorkSink{},
				// Stop only after every scripted conflict has been seen,
				// proving a conflict does not end the run.
				Wait: func(context.Context, time.Duration) error {
					if source.drained() {
						return context.Canceled
					}
					return nil
				},
			}

			if err := service.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v, want nil: a claim conflict must not stop the projector", err)
			}
			if !source.drained() {
				t.Fatalf("claim calls stopped before the %d scripted conflicts drained", source.conflicts)
			}
		})
	}
}

func TestServiceRunStillStopsOnNonConflictClaimError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("claim projector work: relation does not exist")
	for _, workers := range []int{1, 2} {
		service := Service{
			Workers:    workers,
			WorkSource: claimErrorSource{err: wantErr},
			FactStore:  &stubFactStore{},
			Runner:     &stubProjectionRunner{},
			WorkSink:   &stubProjectorWorkSink{},
			Wait:       func(context.Context, time.Duration) error { return nil },
		}
		if err := service.Run(context.Background()); !errors.Is(err, wantErr) {
			t.Fatalf("workers=%d Run() error = %v, want %v", workers, err, wantErr)
		}
	}
}

type claimErrorSource struct{ err error }

func (s claimErrorSource) Claim(context.Context) (ScopeGenerationWork, bool, error) {
	return ScopeGenerationWork{}, false, s.err
}
