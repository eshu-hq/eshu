// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build ifarepodependencyproof

package reducer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIfaRepoDependencyProofWorkersOverlapDistinctAcceptanceUnits(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	rows := make([]SharedProjectionIntentRow, 0, 4)
	for i := range 4 {
		repoID := fmt.Sprintf("repository:r_source_%d", i)
		rows = append(rows, repoDependencyIntentRow(
			fmt.Sprintf("intent-%d", i),
			fmt.Sprintf("scope-%d", i),
			repoID,
			repoID,
			fmt.Sprintf("run-%d", i),
			fmt.Sprintf("gen-%d", i),
			now.Add(time.Duration(i)*time.Second),
			map[string]any{
				"repo_id":           repoID,
				"target_repo_id":    "repository:r_shared_target",
				"relationship_type": "DEPENDS_ON",
				"evidence_source":   crossRepoEvidenceSource,
			},
		))
	}

	store := newProofRepoDependencyStore(rows)
	store.onAllCompleted = cancel
	writer := &overlapRecordingRepoDependencyWriter{delay: 40 * time.Millisecond}
	runner := RepoDependencyProjectionRunner{
		IntentReader: store,
		LeaseManager: store,
		EdgeWriter:   writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-" + strings.TrimPrefix(key.SourceRunID, "run-"), true
		},
		Config: RepoDependencyProjectionRunnerConfig{
			Workers:      4,
			PollInterval: time.Millisecond,
			LeaseTTL:     time.Second,
			BatchLimit:   100,
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := writer.maxConcurrent(); got < 2 {
		t.Fatalf("max concurrent repo dependency writes = %d, want >= 2", got)
	}
	if got := store.completedCount(); got != len(rows) {
		t.Fatalf("completed intents = %d, want %d", got, len(rows))
	}
	if store.usedGlobalLease() {
		t.Fatal("proof workers used the production global 0/1 lease; worker count remained inert")
	}
}

func TestIfaRepoDependencyProofWorkersKeepWholeAcceptanceUnitTogether(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	now := time.Date(2026, time.July, 13, 13, 0, 0, 0, time.UTC)
	const repoID = "repository:r_source_atomic"
	rows := make([]SharedProjectionIntentRow, 0, 3)
	for i := range 3 {
		rows = append(rows, repoDependencyIntentRow(
			fmt.Sprintf("atomic-intent-%d", i),
			fmt.Sprintf("atomic-scope-%d", i),
			repoID,
			repoID,
			fmt.Sprintf("atomic-run-%d", i),
			fmt.Sprintf("atomic-gen-%d", i),
			now.Add(time.Duration(i)*time.Second),
			map[string]any{
				"repo_id":           repoID,
				"target_repo_id":    fmt.Sprintf("repository:r_target_%d", i),
				"relationship_type": "DEPENDS_ON",
				"evidence_source":   crossRepoEvidenceSource,
			},
		))
	}
	store := newProofRepoDependencyStore(rows)
	store.onAllCompleted = cancel
	writer := &overlapRecordingRepoDependencyWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: store,
		LeaseManager: store,
		EdgeWriter:   writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "atomic-gen-" + strings.TrimPrefix(key.SourceRunID, "atomic-run-"), true
		},
		Config: RepoDependencyProjectionRunnerConfig{
			Workers:      4,
			PollInterval: time.Millisecond,
			LeaseTTL:     time.Second,
			BatchLimit:   100,
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := writer.writeBatchSizes(), []int{3}; !equalIntSlices(got, want) {
		t.Fatalf("write batch sizes = %v, want %v; one source acceptance unit was split", got, want)
	}
}

type proofRepoDependencyStore struct {
	mu             sync.Mutex
	rows           []SharedProjectionIntentRow
	leases         map[string]string
	leaseKeys      []string
	completed      map[string]struct{}
	onAllCompleted func()
}

func newProofRepoDependencyStore(rows []SharedProjectionIntentRow) *proofRepoDependencyStore {
	return &proofRepoDependencyStore{
		rows:      append([]SharedProjectionIntentRow(nil), rows...),
		leases:    make(map[string]string),
		completed: make(map[string]struct{}),
	}
}

func (s *proofRepoDependencyStore) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]SharedProjectionIntentRow, 0, len(s.rows))
	for _, row := range s.rows {
		if _, ok := s.completed[row.IntentID]; ok {
			continue
		}
		rows = append(rows, row)
	}
	return truncateRowsForLimit(rows, limit), nil
}

func (s *proofRepoDependencyStore) ListAcceptanceUnitDomainIntents(_ context.Context, acceptanceUnitID, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]SharedProjectionIntentRow, 0, len(s.rows))
	for _, row := range s.rows {
		if row.AcceptanceUnitID != acceptanceUnitID {
			continue
		}
		if _, ok := s.completed[row.IntentID]; ok {
			continue
		}
		rows = append(rows, row)
	}
	return truncateRowsForLimit(rows, limit), nil
}

func (s *proofRepoDependencyStore) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	s.mu.Lock()
	for _, intentID := range intentIDs {
		s.completed[intentID] = struct{}{}
	}
	done := len(s.completed) == len(s.rows)
	onAllCompleted := s.onAllCompleted
	s.mu.Unlock()
	if done && onAllCompleted != nil {
		onAllCompleted()
	}
	return nil
}

func (s *proofRepoDependencyStore) ClaimPartitionLease(_ context.Context, domain string, partitionID, partitionCount int, owner string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d/%d", domain, partitionID, partitionCount)
	s.leaseKeys = append(s.leaseKeys, key)
	current, held := s.leases[key]
	if held && current != owner {
		return false, nil
	}
	s.leases[key] = owner
	return true, nil
}

func (s *proofRepoDependencyStore) ReleasePartitionLease(_ context.Context, domain string, partitionID, partitionCount int, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s/%d/%d", domain, partitionID, partitionCount)
	if s.leases[key] == owner {
		delete(s.leases, key)
	}
	return nil
}

func (s *proofRepoDependencyStore) completedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.completed)
}

func (s *proofRepoDependencyStore) usedGlobalLease() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := fmt.Sprintf("%s/0/1", DomainRepoDependency)
	for _, key := range s.leaseKeys {
		if key == want {
			return true
		}
	}
	return false
}

type overlapRecordingRepoDependencyWriter struct {
	mu      sync.Mutex
	current int
	max     int
	delay   time.Duration
	batches []int
}

func (w *overlapRecordingRepoDependencyWriter) RetractEdges(context.Context, string, []SharedProjectionIntentRow, string) error {
	return nil
}

func (w *overlapRecordingRepoDependencyWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	w.mu.Lock()
	w.current++
	w.batches = append(w.batches, len(rows))
	if w.current > w.max {
		w.max = w.current
	}
	w.mu.Unlock()
	time.Sleep(w.delay)
	w.mu.Lock()
	w.current--
	w.mu.Unlock()
	return nil
}

func (w *overlapRecordingRepoDependencyWriter) maxConcurrent() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.max
}

func (w *overlapRecordingRepoDependencyWriter) writeBatchSizes() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]int(nil), w.batches...)
}

func equalIntSlices(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
