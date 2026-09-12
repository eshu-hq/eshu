// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"sort"
	"sync"
	"time"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

type fakeCodeCallIntentStore struct {
	mu                      sync.Mutex
	pendingByDomain         []sharedintent.Row
	pendingByAcceptance     map[string][]sharedintent.Row
	marked                  []string
	leaseGranted            bool
	claims                  int
	afterClaim              func(int)
	domainLimitRequests     []int
	acceptanceLimitRequests []int
	acceptanceResponder     func(key sharedintent.AcceptanceKey, limit int) ([]sharedintent.Row, error)
}

type historyAwareCodeCallIntentStore struct {
	*fakeCodeCallIntentStore
	hasCompleted                  bool
	hasCompletedCurrentRun        bool
	completedCurrentRunPartitions map[string]bool
	completedCurrentRunRefresh    map[string]bool
	historyErr                    error
}

type fenceAwareCodeCallIntentStore struct {
	*fakeCodeCallIntentStore
	blockedByFence bool
	checkedRows    []string
}

type staticReducerGraphDrain struct {
	active bool
	err    error
}

func (s staticReducerGraphDrain) HasActiveReducerGraphWork(context.Context) (bool, error) {
	return s.active, s.err
}

func (h *historyAwareCodeCallIntentStore) HasCompletedAcceptanceUnitDomainIntents(
	context.Context,
	sharedintent.AcceptanceKey,
	string,
) (bool, error) {
	if h.historyErr != nil {
		return false, h.historyErr
	}
	return h.hasCompleted, nil
}

func (h *historyAwareCodeCallIntentStore) HasCompletedAcceptanceUnitSourceRunDomainIntents(
	context.Context,
	sharedintent.AcceptanceKey,
	string,
) (bool, error) {
	if h.historyErr != nil {
		return false, h.historyErr
	}
	return h.hasCompletedCurrentRun, nil
}

func (h *historyAwareCodeCallIntentStore) HasCompletedAcceptanceUnitSourceRunPartitionDomainIntents(
	_ context.Context,
	_ sharedintent.AcceptanceKey,
	partitionKey string,
	_ string,
) (bool, error) {
	if h.historyErr != nil {
		return false, h.historyErr
	}
	if h.completedCurrentRunPartitions != nil {
		return h.completedCurrentRunPartitions[partitionKey], nil
	}
	return h.hasCompletedCurrentRun, nil
}

func (h *historyAwareCodeCallIntentStore) HasCompletedAcceptanceUnitSourceRunRefreshDomainIntents(
	_ context.Context,
	_ sharedintent.AcceptanceKey,
	filePaths []string,
	_ string,
) (bool, error) {
	if h.historyErr != nil {
		return false, h.historyErr
	}
	if len(filePaths) == 0 || len(h.completedCurrentRunRefresh) == 0 {
		return false, nil
	}
	for _, filePath := range filePaths {
		if !h.completedCurrentRunRefresh[filePath] {
			return false, nil
		}
	}
	return true, nil
}

func (f *fenceAwareCodeCallIntentStore) CodeCallProjectionRowBlockedByRepoFence(
	_ context.Context,
	_ sharedintent.AcceptanceKey,
	row sharedintent.Row,
	_ string,
) (bool, error) {
	f.checkedRows = append(f.checkedRows, row.IntentID)
	return f.blockedByFence, nil
}

func codeCallProjectionTestRow(intentID, generationID string, createdAt time.Time) sharedintent.Row {
	return sharedintent.Row{
		IntentID:         intentID,
		ProjectionDomain: reducercontract.DomainCodeCalls,
		PartitionKey:     "caller->callee",
		ScopeID:          "scope-a",
		AcceptanceUnitID: "repo-a",
		RepositoryID:     "repo-a",
		SourceRunID:      "run-1",
		GenerationID:     generationID,
		Payload: map[string]any{
			"repo_id":          "repo-a",
			"caller_entity_id": "caller",
			"callee_entity_id": "callee",
			"evidence_source":  codecall.EvidenceSource,
		},
		CreatedAt: createdAt,
	}
}

func (f *fakeCodeCallIntentStore) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]sharedintent.Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.domainLimitRequests = append(f.domainLimitRequests, limit)
	rows := make([]sharedintent.Row, 0, len(f.pendingByDomain))
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

func (f *fakeCodeCallIntentStore) ListPendingAcceptanceUnitIntents(_ context.Context, key sharedintent.AcceptanceKey, _ string, limit int) ([]sharedintent.Row, error) {
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
	rows := make([]sharedintent.Row, 0, len(sourceRows))
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

func (f *fakeCodeCallIntentStore) claimsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.claims
}

type recordingCodeCallProjectionEdgeWriter struct {
	retractCalls []recordedProjectionCall
	writeCalls   []recordedProjectionCall
}

type recordedProjectionCall struct {
	rows           []sharedintent.Row
	evidenceSource string
}

func (r *recordingCodeCallProjectionEdgeWriter) RetractEdges(_ context.Context, _ string, rows []sharedintent.Row, evidenceSource string) error {
	r.retractCalls = append(r.retractCalls, recordedProjectionCall{
		rows:           append([]sharedintent.Row(nil), rows...),
		evidenceSource: evidenceSource,
	})
	return nil
}

func (r *recordingCodeCallProjectionEdgeWriter) WriteEdges(_ context.Context, _ string, rows []sharedintent.Row, evidenceSource string) (sharedintent.WriteReport, error) {
	r.writeCalls = append(r.writeCalls, recordedProjectionCall{
		rows:           append([]sharedintent.Row(nil), rows...),
		evidenceSource: evidenceSource,
	})
	return sharedintent.WriteReport{}, nil
}

type flakyCodeCallProjectionEdgeWriter struct {
	recordingCodeCallProjectionEdgeWriter
	err             error
	retractFailures int
	writeFailures   int
}

func (r *flakyCodeCallProjectionEdgeWriter) RetractEdges(ctx context.Context, domain string, rows []sharedintent.Row, evidenceSource string) error {
	if r.retractFailures > 0 {
		r.retractFailures--
		return r.err
	}
	return r.recordingCodeCallProjectionEdgeWriter.RetractEdges(ctx, domain, rows, evidenceSource)
}

func (r *flakyCodeCallProjectionEdgeWriter) WriteEdges(ctx context.Context, domain string, rows []sharedintent.Row, evidenceSource string) (sharedintent.WriteReport, error) {
	if r.writeFailures > 0 {
		r.writeFailures--
		return sharedintent.WriteReport{}, r.err
	}
	return r.recordingCodeCallProjectionEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
}

type blockingCodeCallProjectionEdgeWriter struct {
	recordingCodeCallProjectionEdgeWriter
	release <-chan struct{}
}

func (r *blockingCodeCallProjectionEdgeWriter) WriteEdges(ctx context.Context, domain string, rows []sharedintent.Row, evidenceSource string) (sharedintent.WriteReport, error) {
	select {
	case <-r.release:
	case <-ctx.Done():
		return sharedintent.WriteReport{}, ctx.Err()
	}
	return r.recordingCodeCallProjectionEdgeWriter.WriteEdges(ctx, domain, rows, evidenceSource)
}
