// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runwatermark

import (
	"context"
	"sync"
)

// InMemoryStore is a process-local Store. It has no durability across
// process restarts or visibility across replicas: it is safe as an interim
// mitigation for a single long-running collector process (catching gaps
// between consecutive claim cycles serviced by that process), and it is
// what the test suite uses to exercise gap detection without a database.
// Production deployments that need cross-restart, cross-replica gap
// detection must wire a durable Store (for example a Postgres-backed
// implementation) through SourceConfig.Watermarks instead.
type InMemoryStore struct {
	mu   sync.Mutex
	rows map[Key]Watermark
}

// NewInMemoryStore returns an empty in-memory watermark store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{rows: make(map[Key]Watermark)}
}

// Load returns the stored watermark for key, if any.
func (s *InMemoryStore) Load(_ context.Context, key Key) (Watermark, bool, error) {
	if err := key.Validate(); err != nil {
		return Watermark{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.rows[key]
	return value, ok, nil
}

// Save upserts value, rejecting a fencing token strictly older than the
// stored row's fencing token with ErrStaleFence. A fencing token equal to
// the stored row's is accepted (idempotent redelivery).
func (s *InMemoryStore) Save(_ context.Context, value Watermark) error {
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.rows[value.Key]; ok && value.FencingToken < existing.FencingToken {
		return ErrStaleFence
	}
	s.rows[value.Key] = value
	return nil
}

var _ Store = (*InMemoryStore)(nil)
