// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerSelectsPartitionCandidatesWithoutDomainScan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 8, 0, 0, 0, time.UTC)
	partitionKey := "code-calls:v1:files:repo-a:src/caller.go"
	partitionID := mustPartitionForKey(t, partitionKey, 8)
	reader := &fakePartitionCandidateIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByAcceptance: map[string][]SharedProjectionIntentRow{
				"scope-a|repo-a|run-1": {
					{
						IntentID:         "candidate-1",
						ProjectionDomain: DomainCodeCalls,
						PartitionKey:     partitionKey,
						ScopeID:          "scope-a",
						AcceptanceUnitID: "repo-a",
						RepositoryID:     "repo-a",
						SourceRunID:      "run-1",
						GenerationID:     "gen-1",
						CreatedAt:        now,
					},
				},
			},
		},
		partitionRows: []SharedProjectionIntentRow{
			{
				IntentID:         "candidate-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     partitionKey,
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:     10,
			PartitionCount: 8,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		partitionID,
		8,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if got, want := result.PartitionKey, partitionKey; got != want {
		t.Fatalf("PartitionKey = %q, want %q", got, want)
	}
	if got, want := result.Key.AcceptanceUnitID, "repo-a"; got != want {
		t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
	}
	if len(reader.domainLimitRequests) != 0 {
		t.Fatalf("domainLimitRequests = %v, want no global domain scan", reader.domainLimitRequests)
	}
	if got, want := reader.partitionRequests, []partitionCandidateRequest{{domain: DomainCodeCalls, partitionID: partitionID, partitionCount: 8, limit: 10}}; !equalPartitionCandidateRequests(got, want) {
		t.Fatalf("partitionRequests = %#v, want %#v", got, want)
	}
}

func TestCodeCallProjectionRunnerReadsUnhashedPendingRowsWithoutDomainScan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 8, 30, 0, 0, time.UTC)
	partitionKey := "code-calls:v1:files:repo-a:src/legacy.go"
	partitionID := mustPartitionForKey(t, partitionKey, 8)
	reader := &fakePartitionCandidateIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByAcceptance: map[string][]SharedProjectionIntentRow{
				"scope-a|repo-a|run-1": {
					{
						IntentID:         "legacy-unhashed",
						ProjectionDomain: DomainCodeCalls,
						PartitionKey:     partitionKey,
						ScopeID:          "scope-a",
						AcceptanceUnitID: "repo-a",
						RepositoryID:     "repo-a",
						SourceRunID:      "run-1",
						GenerationID:     "gen-1",
						CreatedAt:        now,
					},
				},
			},
		},
		legacyRows: []SharedProjectionIntentRow{
			{
				IntentID:         "legacy-unhashed",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     partitionKey,
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:     10,
			PartitionCount: 8,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		partitionID,
		8,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if got, want := result.PartitionKey, partitionKey; got != want {
		t.Fatalf("PartitionKey = %q, want %q", got, want)
	}
	if got, want := len(reader.partitionRequests), 1; got != want {
		t.Fatalf("partitionRequests = %d, want %d", got, want)
	}
	if len(reader.domainLimitRequests) != 0 {
		t.Fatalf("domainLimitRequests = %v, want no global domain scan", reader.domainLimitRequests)
	}
	if got, want := reader.legacyRequests, []legacyCandidateRequest{{domain: DomainCodeCalls, limit: 250000}}; !equalLegacyCandidateRequests(got, want) {
		t.Fatalf("legacyRequests = %#v, want %#v", got, want)
	}
}

func TestCodeCallProjectionRunnerEmptyPartitionDoesNotFallbackToDomainScan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 8, 45, 0, 0, time.UTC)
	reader := &fakePartitionCandidateIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByDomain: []SharedProjectionIntentRow{
				{
					IntentID:         "other-partition",
					ProjectionDomain: DomainCodeCalls,
					PartitionKey:     "code-calls:v1:files:repo-a:src/other.go",
					ScopeID:          "scope-a",
					AcceptanceUnitID: "repo-a",
					RepositoryID:     "repo-a",
					SourceRunID:      "run-1",
					GenerationID:     "gen-1",
					CreatedAt:        now,
				},
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:     10,
			PartitionCount: 8,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		0,
		8,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if result.Key != (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("Key = %#v, want empty miss", result.Key)
	}
	if got, want := len(reader.partitionRequests), 1; got != want {
		t.Fatalf("partitionRequests = %d, want %d", got, want)
	}
	if len(reader.domainLimitRequests) != 0 {
		t.Fatalf("domainLimitRequests = %v, want no global domain scan", reader.domainLimitRequests)
	}
}

func TestCodeCallProjectionRunnerIndexedCandidateFenceSpansAcceptanceUnit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 19, 9, 0, 0, 0, time.UTC)
	partitionCount := 8
	wholeRow := codeCallProjectionWholeScopeRow("whole-refresh", "repo-a", now)
	wholePartitionID := mustPartitionForKey(t, wholeRow.PartitionKey, partitionCount)
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	filePartitionID := mustPartitionForKey(t, filePartition, partitionCount)
	if filePartitionID == wholePartitionID {
		t.Fatalf("test partition keys mapped to same partition %d; choose different fixture paths", filePartitionID)
	}
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now.Add(time.Millisecond),
	)
	reader := &fakePartitionCandidateIntentStore{
		fakeCodeCallIntentStore: &fakeCodeCallIntentStore{
			pendingByAcceptance: map[string][]SharedProjectionIntentRow{
				"scope-a|repo-a|run-1": {wholeRow, fileRow},
			},
			leaseGranted: true,
		},
		partitionRows: []SharedProjectionIntentRow{wholeRow, fileRow},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:     10,
			PartitionCount: partitionCount,
		},
	}

	result, err := runner.processPartitionOnce(context.Background(), now, filePartitionID, partitionCount)
	if err != nil {
		t.Fatalf("processPartitionOnce(file) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d, want file partition blocked by earlier whole scope", result.ProcessedIntents)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want none while whole scope fences file partitions", reader.marked)
	}
	if len(reader.domainLimitRequests) != 0 {
		t.Fatalf("domainLimitRequests = %v, want indexed candidate path without domain scan", reader.domainLimitRequests)
	}
	if len(reader.acceptanceLimitRequests) == 0 {
		t.Fatal("acceptanceLimitRequests = nil, want full acceptance-unit fence check")
	}
}

type partitionCandidateRequest struct {
	domain         string
	partitionID    int
	partitionCount int
	limit          int
}

type legacyCandidateRequest struct {
	domain string
	limit  int
}

type fakePartitionCandidateIntentStore struct {
	*fakeCodeCallIntentStore
	partitionRows     []SharedProjectionIntentRow
	legacyRows        []SharedProjectionIntentRow
	partitionRequests []partitionCandidateRequest
	legacyRequests    []legacyCandidateRequest
}

func (f *fakePartitionCandidateIntentStore) ListPendingDomainPartitionIntents(
	_ context.Context,
	domain string,
	partitionID int,
	partitionCount int,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	f.partitionRequests = append(f.partitionRequests, partitionCandidateRequest{
		domain:         domain,
		partitionID:    partitionID,
		partitionCount: partitionCount,
		limit:          limit,
	})

	rows := make([]SharedProjectionIntentRow, 0, len(f.partitionRows))
	for _, row := range f.partitionRows {
		if row.CompletedAt != nil || row.ProjectionDomain != domain {
			continue
		}
		if !codeCallProjectionPartitionMatches(row, partitionID, partitionCount) {
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

func (f *fakePartitionCandidateIntentStore) ListPendingDomainUnhashedIntents(
	_ context.Context,
	domain string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	f.legacyRequests = append(f.legacyRequests, legacyCandidateRequest{
		domain: domain,
		limit:  limit,
	})

	rows := make([]SharedProjectionIntentRow, 0, len(f.legacyRows))
	for _, row := range f.legacyRows {
		if row.CompletedAt != nil || row.ProjectionDomain != domain {
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

func equalPartitionCandidateRequests(a, b []partitionCandidateRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalLegacyCandidateRequests(a, b []legacyCandidateRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
