// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedulereplay

import (
	"context"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// ScheduledWorkSource is a deterministic, in-memory reducer work source that
// delivers a pre-scripted sequence of intents in exact order. It replaces the
// production FOR UPDATE SKIP LOCKED Postgres claim path so an ordering scenario
// (in-order, adversarial reverse, duplicate delivery, interleaved conflict keys)
// is reproducible byte-for-byte with no database. It implements both
// reducer.WorkSource (single Claim) and reducer.BatchWorkSource (ClaimBatch), so
// the same scenario can drive the sequential loop or the real concurrent batch
// claim path.
//
// All methods are safe for concurrent use so the concurrent reducer worker pool
// can claim from one source.
type ScheduledWorkSource struct {
	mu         sync.Mutex
	schedule   []reducer.Intent
	cursor     int
	batchCalls int
}

// NewScheduledWorkSource returns a source that delivers the given intents in the
// exact order provided. The schedule is copied, so the caller may reuse its
// slice. Duplicate intents (same IntentID appearing more than once) are
// delivered each time they appear, modeling duplicate queue delivery.
func NewScheduledWorkSource(schedule []reducer.Intent) *ScheduledWorkSource {
	cp := make([]reducer.Intent, len(schedule))
	copy(cp, schedule)
	return &ScheduledWorkSource{schedule: cp}
}

// Claim returns the next scripted intent, or ok=false once the schedule is
// exhausted. It never blocks and never errors: the schedule is fixed up front.
func (s *ScheduledWorkSource) Claim(_ context.Context) (reducer.Intent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursor >= len(s.schedule) {
		return reducer.Intent{}, false, nil
	}
	intent := s.schedule[s.cursor]
	s.cursor++
	return intent, true, nil
}

// ClaimBatch returns up to limit scripted intents in order, or an empty slice
// once the schedule is exhausted. It records that the batch path was used so a
// scenario can prove the concurrent BatchWorkSource path actually ran.
func (s *ScheduledWorkSource) ClaimBatch(_ context.Context, limit int) ([]reducer.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls++
	if s.cursor >= len(s.schedule) || limit <= 0 {
		return nil, nil
	}
	end := s.cursor + limit
	if end > len(s.schedule) {
		end = len(s.schedule)
	}
	batch := make([]reducer.Intent, end-s.cursor)
	copy(batch, s.schedule[s.cursor:end])
	s.cursor = end
	return batch, nil
}

// Drained reports whether every scripted intent has been claimed.
func (s *ScheduledWorkSource) Drained() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cursor >= len(s.schedule)
}

// ClaimBatchCalls returns how many times ClaimBatch was invoked, including the
// final empty drain calls. A non-zero count proves the batch claim path ran.
func (s *ScheduledWorkSource) ClaimBatchCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.batchCalls
}
