// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"time"
)

// fakeSharedIntentReader, fakeLeaseManager, and fakeEdgeWriter mirror the
// test doubles [worker]'s own tests define (issue #6061: their subject moved
// there, but service_test.go and the remaining shared_projection_runner_*
// tests here still need local doubles of their own, since a test double is
// not part of either package's exported surface).

type fakeSharedIntentReader struct {
	mu      sync.Mutex
	intents []SharedProjectionIntentRow
	marked  []string
}

func (f *fakeSharedIntentReader) ListPendingDomainIntents(_ context.Context, domain string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var result []SharedProjectionIntentRow
	for _, row := range f.intents {
		if row.ProjectionDomain == domain && row.CompletedAt == nil {
			result = append(result, row)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakeSharedIntentReader) MarkIntentsCompleted(_ context.Context, intentIDs []string, completedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.marked = append(f.marked, intentIDs...)
	idSet := make(map[string]struct{}, len(intentIDs))
	for _, id := range intentIDs {
		idSet[id] = struct{}{}
	}
	for i := range f.intents {
		if _, ok := idSet[f.intents[i].IntentID]; ok {
			t := completedAt
			f.intents[i].CompletedAt = &t
		}
	}
	return nil
}

type fakeLeaseManager struct {
	mu      sync.Mutex
	claims  int
	granted bool
}

func (f *fakeLeaseManager) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims++
	return f.granted, nil
}

func (f *fakeLeaseManager) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}

type fakeEdgeWriter struct {
	mu        sync.Mutex
	writes    int
	retracts  int
	writeRows []SharedProjectionIntentRow
	// writeErr, when non-nil, is returned by WriteEdges instead of a
	// successful write -- used to simulate a graph-executor-seam failure
	// (e.g. the ifafaultinjection fail-graph-write-once-then-succeed fault)
	// for TestSharedProjectionRunnerLogsPartitionProcessingError.
	writeErr error
}

func (f *fakeEdgeWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) (SharedProjectionWriteReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.writeErr != nil {
		return SharedProjectionWriteReport{}, f.writeErr
	}
	f.writeRows = append(f.writeRows, rows...)
	return SharedProjectionWriteReport{}, nil
}

func (f *fakeEdgeWriter) RetractEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retracts++
	return nil
}
