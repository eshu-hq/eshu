// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRepoDependencyProjectionRunnerConfigDefaults(t *testing.T) {
	t.Parallel()

	cfg := RepoDependencyProjectionRunnerConfig{}
	if got := cfg.pollInterval(); got != defaultSharedPollInterval {
		t.Fatalf("pollInterval() = %v, want %v", got, defaultSharedPollInterval)
	}
	if got := cfg.leaseTTL(); got != defaultRepoDependencyProjectionLeaseTTL {
		t.Fatalf("leaseTTL() = %v, want %v", got, defaultRepoDependencyProjectionLeaseTTL)
	}
	if got := cfg.batchLimit(); got != defaultBatchLimit {
		t.Fatalf("batchLimit() = %d, want %d", got, defaultBatchLimit)
	}
	if got := cfg.leaseOwner(); got != defaultRepoDependencyLeaseOwner {
		t.Fatalf("leaseOwner() = %q, want %q", got, defaultRepoDependencyLeaseOwner)
	}
}

func TestRepoDependencyProjectionRunnerValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runner RepoDependencyProjectionRunner
	}{
		{
			name: "missing intent reader",
			runner: RepoDependencyProjectionRunner{
				LeaseManager: &fakeRepoDependencyIntentStore{leaseGranted: true},
				EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
				AcceptedGen:  acceptedGenerationFixed("", false),
			},
		},
		{
			name: "missing lease manager",
			runner: RepoDependencyProjectionRunner{
				IntentReader: &fakeRepoDependencyIntentStore{leaseGranted: true},
				EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
				AcceptedGen:  acceptedGenerationFixed("", false),
			},
		},
		{
			name: "missing edge writer",
			runner: RepoDependencyProjectionRunner{
				IntentReader: &fakeRepoDependencyIntentStore{leaseGranted: true},
				LeaseManager: &fakeRepoDependencyIntentStore{leaseGranted: true},
				AcceptedGen:  acceptedGenerationFixed("", false),
			},
		},
		{
			name: "missing accepted generation lookup",
			runner: RepoDependencyProjectionRunner{
				IntentReader: &fakeRepoDependencyIntentStore{leaseGranted: true},
				LeaseManager: &fakeRepoDependencyIntentStore{leaseGranted: true},
				EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
			},
		},
		{
			name: "single worker missing acceptance unit gate",
			runner: RepoDependencyProjectionRunner{
				IntentReader: &fakeRepoDependencyIntentStore{leaseGranted: true},
				LeaseManager: &fakeRepoDependencyIntentStore{leaseGranted: true},
				EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
				AcceptedGen:  acceptedGenerationFixed("", false),
				Config:       RepoDependencyProjectionRunnerConfig{Workers: 1},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.runner.validate(); err == nil {
				t.Fatal("validate() error = nil, want non-nil")
			}
		})
	}
}

func TestRepoDependencyProjectionRunnerProcessOnceRejectsMissingAcceptanceUnitGate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	row := repoDependencyIntentRow(
		"intent-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{row},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {row}},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       RepoDependencyProjectionRunnerConfig{Workers: 1},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "acceptance unit gate is required") {
		t.Fatalf("processOnce() error = %v, want missing acceptance unit gate error", err)
	}
	if got := len(writer.writeCalls); got != 0 {
		t.Fatalf("write calls = %d, want 0 without acceptance unit gate", got)
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked intents = %d, want 0 without acceptance unit gate", got)
	}
}

func TestRepoDependencyProjectionRunnerLoadAllAcceptanceUnitIntentsRejectsOversizedSlice(t *testing.T) {
	t.Parallel()

	reader := &fakeRepoDependencyIntentStore{
		acceptanceUnitResponder: func(_ string, limit int) ([]SharedProjectionIntentRow, error) {
			rows := make([]SharedProjectionIntentRow, limit)
			for i := range rows {
				rows[i] = repoDependencyIntentRow(
					"intent", "scope-a", "repository:r_repo_a", "repository:r_repo_a", "run-1", "gen-1", time.Now().UTC(),
					map[string]any{
						"repo_id":           "repository:r_repo_a",
						"target_repo_id":    "repository:r_target",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   crossRepoEvidenceSource,
					},
				)
			}
			return rows, nil
		},
	}
	runner := RepoDependencyProjectionRunner{
		IntentReader: reader,
		Config:       RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	_, err := runner.loadAllAcceptanceUnitIntents(context.Background(), reader, "repository:r_repo_a")
	if err == nil {
		t.Fatal("loadAllAcceptanceUnitIntents() error = nil, want non-nil")
	}
	if len(reader.acceptanceLimitRequests) < 2 {
		t.Fatalf("acceptanceLimitRequests = %v, want growth up to cap", reader.acceptanceLimitRequests)
	}
	if got, want := reader.acceptanceLimitRequests[len(reader.acceptanceLimitRequests)-1], maxRepoDependencyAcceptanceScanLimit; got != want {
		t.Fatalf("final acceptance scan limit = %d, want %d", got, want)
	}
}

type fakeRepoDependencyIntentStore struct {
	mu                        sync.Mutex
	pendingByDomain           []SharedProjectionIntentRow
	pendingByAcceptanceUnit   map[string][]SharedProjectionIntentRow
	marked                    []string
	leaseGranted              bool
	leaseClaims               int
	leaseClaimHook            func(int)
	domainLimitRequests       []int
	acceptanceLimitRequests   []int
	acceptanceUnitRequests    []string
	acceptanceUnitResponder   func(acceptanceUnitID string, limit int) ([]SharedProjectionIntentRow, error)
	domainIntentListError     error
	acceptanceIntentListError error
}

func (f *fakeRepoDependencyIntentStore) ListPendingDomainIntents(_ context.Context, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.domainIntentListError != nil {
		return nil, f.domainIntentListError
	}
	f.domainLimitRequests = append(f.domainLimitRequests, limit)
	return truncatePendingRows(f.pendingByDomain, limit), nil
}

func (f *fakeRepoDependencyIntentStore) ListAcceptanceUnitDomainIntents(_ context.Context, acceptanceUnitID, _ string, limit int) ([]SharedProjectionIntentRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.acceptanceIntentListError != nil {
		return nil, f.acceptanceIntentListError
	}
	f.acceptanceLimitRequests = append(f.acceptanceLimitRequests, limit)
	f.acceptanceUnitRequests = append(f.acceptanceUnitRequests, acceptanceUnitID)
	if f.acceptanceUnitResponder != nil {
		return f.acceptanceUnitResponder(acceptanceUnitID, limit)
	}
	return truncateAcceptanceUnitRows(f.pendingByAcceptanceUnit[acceptanceUnitID], limit), nil
}

func (f *fakeRepoDependencyIntentStore) MarkIntentsCompleted(_ context.Context, intentIDs []string, _ time.Time) error {
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
	for key := range f.pendingByAcceptanceUnit {
		for i := range f.pendingByAcceptanceUnit[key] {
			if _, ok := markSet[f.pendingByAcceptanceUnit[key][i].IntentID]; ok {
				f.pendingByAcceptanceUnit[key][i].CompletedAt = &completedAt
			}
		}
	}
	return nil
}

func (f *fakeRepoDependencyIntentStore) ClaimPartitionLease(_ context.Context, _ string, _, _ int, _ string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	f.leaseClaims++
	claims := f.leaseClaims
	leaseGranted := f.leaseGranted
	hook := f.leaseClaimHook
	f.mu.Unlock()
	if hook != nil {
		hook(claims)
	}
	return leaseGranted, nil
}

func (f *fakeRepoDependencyIntentStore) ReleasePartitionLease(_ context.Context, _ string, _, _ int, _ string) error {
	return nil
}

func (f *fakeRepoDependencyIntentStore) WithAcceptanceUnit(
	ctx context.Context,
	_ RepoDependencyAcceptanceUnitGateKey,
	fn func(context.Context, RepoDependencyProjectionIntentReader) error,
) (bool, error) {
	if err := fn(ctx, f); err != nil {
		return true, err
	}
	return true, nil
}

func (f *fakeRepoDependencyIntentStore) claimCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.leaseClaims
}

func truncatePendingRows(rows []SharedProjectionIntentRow, limit int) []SharedProjectionIntentRow {
	filtered := make([]SharedProjectionIntentRow, 0, len(rows))
	for _, row := range rows {
		if row.CompletedAt != nil {
			continue
		}
		filtered = append(filtered, row)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].IntentID < filtered[j].IntentID
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func truncateRowsForLimit(rows []SharedProjectionIntentRow, limit int) []SharedProjectionIntentRow {
	filtered := append([]SharedProjectionIntentRow(nil), rows...)
	sort.SliceStable(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.Before(filtered[j].CreatedAt)
		}
		return filtered[i].IntentID < filtered[j].IntentID
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func truncateAcceptanceUnitRows(rows []SharedProjectionIntentRow, limit int) []SharedProjectionIntentRow {
	return truncateRowsForLimit(rows, limit)
}

func repoDependencyIntentRow(
	intentID string,
	scopeID string,
	acceptanceUnitID string,
	repositoryID string,
	sourceRunID string,
	generationID string,
	createdAt time.Time,
	payload map[string]any,
) SharedProjectionIntentRow {
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     intentID,
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     repositoryID,
		SourceRunID:      sourceRunID,
		GenerationID:     generationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	}
}

func TestRepoDependencyProjectionRunnerRunContinuesAfterCycleError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 13, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			repoDependencyIntentRow(
				"active-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_target_1",
					"relationship_type": "DEPENDS_ON",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
		},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {
				repoDependencyIntentRow(
					"active-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_1",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
			},
		},
		leaseGranted: true,
	}
	writer := &flakyCodeCallProjectionEdgeWriter{
		err:             errors.New("neo4j transient write conflict"),
		retractFailures: 1,
	}
	waits := make([]time.Duration, 0, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := RepoDependencyProjectionRunner{
		IntentReader:       reader,
		LeaseManager:       reader,
		AcceptanceUnitGate: reader,
		EdgeWriter:         writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.AcceptanceUnitID == repoID
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
		Wait: func(_ context.Context, interval time.Duration) error {
			waits = append(waits, interval)
			if len(waits) == 1 {
				return nil
			}
			cancel()
			return context.Canceled
		},
	}

	if err := runner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(reader.marked); got != 1 {
		t.Fatalf("len(marked) = %d, want 1", got)
	}
	if got := len(writer.writeCalls); got != 1 {
		t.Fatalf("len(writeCalls) = %d, want 1", got)
	}
}
