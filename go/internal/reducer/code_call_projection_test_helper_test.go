// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"sync"
	"time"
)

// fakeCodeCallIntentStore, recordingCodeCallProjectionEdgeWriter,
// flakyCodeCallProjectionEdgeWriter, and blockingCodeCallProjectionEdgeWriter
// are local copies of the [projection] package's own test fakes
// (code/call/projection/helpers_test.go), scoped to the root tests that
// still exercise the code-call projection runner/edge-writer shape directly
// (service_test.go, accepted_generation_active_gate_integration_test.go,
// repo_dependency_projection_acceptance_test.go,
// repo_dependency_projection_quarantine_test.go). Go test files cannot share
// unexported symbols across a package boundary (issue #6061).

type fakeCodeCallIntentStore struct {
	mu                      sync.Mutex
	pendingByDomain         []SharedProjectionIntentRow
	pendingByAcceptance     map[string][]SharedProjectionIntentRow
	marked                  []string
	leaseGranted            bool
	claims                  int
	afterClaim              func(int)
	domainLimitRequests     []int
	acceptanceLimitRequests []int
	acceptanceResponder     func(key SharedProjectionAcceptanceKey, limit int) ([]SharedProjectionIntentRow, error)
}

func (f *fakeCodeCallIntentStore) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.domainLimitRequests = append(f.domainLimitRequests, limit)
	rows := make([]SharedProjectionIntentRow, 0, len(f.pendingByDomain))
	for _, row := range f.pendingByDomain {
		if row.CompletedAt != nil {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].IntentID < rows[j].IntentID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (f *fakeCodeCallIntentStore) ListPendingAcceptanceUnitIntents(_ context.Context, key SharedProjectionAcceptanceKey, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.acceptanceLimitRequests = append(f.acceptanceLimitRequests, limit)
	if f.acceptanceResponder != nil {
		return f.acceptanceResponder(key, limit)
	}

	sourceRows, ok := f.pendingByAcceptance[key.ScopeID+"|"+key.AcceptanceUnitID+"|"+key.SourceRunID]
	if !ok && f.pendingByAcceptance == nil {
		sourceRows = f.pendingByDomain
	}
	rows := make([]SharedProjectionIntentRow, 0, len(sourceRows))
	for _, row := range sourceRows {
		if row.CompletedAt != nil {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].IntentID < rows[j].IntentID
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (f *fakeCodeCallIntentStore) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.marked = append(f.marked, intentIDs...)
	completedAt := time.Now().UTC()
	markSet := make(map[string]struct{}, len(intentIDs))
	for _, intentID := range intentIDs {
		markSet[intentID] = struct{}{}
	}
	for i := range f.pendingByDomain {
		if _, ok := markSet[f.pendingByDomain[i].IntentID]; ok {
			f.pendingByDomain[i].CompletedAt = &completedAt
		}
	}
	for key := range f.pendingByAcceptance {
		for i := range f.pendingByAcceptance[key] {
			if _, ok := markSet[f.pendingByAcceptance[key][i].IntentID]; ok {
				f.pendingByAcceptance[key][i].CompletedAt = &completedAt
			}
		}
	}
	return nil
}

func (f *fakeCodeCallIntentStore) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.claims++
	if f.afterClaim != nil {
		f.afterClaim(f.claims)
	}
	return f.leaseGranted, nil
}

func (f *fakeCodeCallIntentStore) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}

type recordingCodeCallProjectionEdgeWriter struct {
	retractCalls []recordedProjectionCall
	writeCalls   []recordedProjectionCall
}

type recordedProjectionCall struct {
	rows           []SharedProjectionIntentRow
	evidenceSource string
}

func (r *recordingCodeCallProjectionEdgeWriter) RetractEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, evidenceSource string) error {
	r.retractCalls = append(r.retractCalls, recordedProjectionCall{
		rows:           append([]SharedProjectionIntentRow(nil), rows...),
		evidenceSource: evidenceSource,
	})
	return nil
}

func (r *recordingCodeCallProjectionEdgeWriter) WriteEdges(_ context.Context, _ string, rows []SharedProjectionIntentRow, evidenceSource string) (SharedProjectionWriteReport, error) {
	r.writeCalls = append(r.writeCalls, recordedProjectionCall{
		rows:           append([]SharedProjectionIntentRow(nil), rows...),
		evidenceSource: evidenceSource,
	})
	return SharedProjectionWriteReport{}, nil
}

type flakyCodeCallProjectionEdgeWriter struct {
	recordingCodeCallProjectionEdgeWriter
	err             error
	retractFailures int
	writeFailures   int
}

func (r *flakyCodeCallProjectionEdgeWriter) RetractEdges(ctx context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string) error {
	if r.retractFailures > 0 {
		r.retractFailures--
		return r.err
	}
	return r.recordingCodeCallProjectionEdgeWriter.RetractEdges(ctx, domain, rows, evidenceSource)
}

func (r *flakyCodeCallProjectionEdgeWriter) WriteEdges(ctx context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string) (SharedProjectionWriteReport, error) {
	if r.writeFailures > 0 {
		r.writeFailures--
		return SharedProjectionWriteReport{}, r.err
	}
	return r.recordingCodeCallProjectionEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
}

type blockingCodeCallProjectionEdgeWriter struct {
	recordingCodeCallProjectionEdgeWriter
	release <-chan struct{}
}

func (r *blockingCodeCallProjectionEdgeWriter) WriteEdges(ctx context.Context, domain string, rows []SharedProjectionIntentRow, evidenceSource string) (SharedProjectionWriteReport, error) {
	select {
	case <-r.release:
	case <-ctx.Done():
		return SharedProjectionWriteReport{}, ctx.Err()
	}
	return r.recordingCodeCallProjectionEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
}
