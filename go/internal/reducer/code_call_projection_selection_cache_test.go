// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerReusesRefreshFenceRowsAcrossWidenedCandidateWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 10, 0, 0, 0, time.UTC)
	blockedRow := codeCallProjectionDeltaPartitionRow(
		"blocked-edge",
		"code-calls:v1:files:repo-a:src/blocked.go",
		"repo-a",
		"/repo/src/blocked.go",
		now,
	)
	readyRow := codeCallProjectionDeltaPartitionRow(
		"ready-edge",
		"code-calls:v1:files:repo-a:src/ready.go",
		"repo-a",
		"/repo/src/ready.go",
		now.Add(time.Millisecond),
	)
	refreshRow := codeCallProjectionFileRefreshRow(
		"covering-refresh",
		"code-calls:v1:files:repo-a:src/blocked-refresh.go",
		"repo-a",
		[]string{"/repo/src/blocked.go"},
		now.Add(2*time.Millisecond),
	)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{blockedRow, readyRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {blockedRow, readyRow, refreshRow},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          1,
			AcceptanceScanLimit: 10,
			PartitionCount:      1,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		0,
		1,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if got, want := result.PartitionKey, readyRow.PartitionKey; got != want {
		t.Fatalf("PartitionKey = %q, want %q", got, want)
	}
	if got, want := reader.domainLimitRequests, []int{1, 2}; !slices.Equal(got, want) {
		t.Fatalf("domainLimitRequests = %v, want %v", got, want)
	}
	if got, want := reader.acceptanceLimitRequests, []int{1, 2, 4}; !slices.Equal(got, want) {
		t.Fatalf("acceptanceLimitRequests = %v, want one cached refresh-fence scan %v", got, want)
	}
}

func TestCodeCallProjectionRunnerReusesReadinessAcrossWidenedCandidateWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 16, 10, 30, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "blocked-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->blocked",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-blocked",
				RepositoryID:     "repo-blocked",
				SourceRunID:      "run-1",
				GenerationID:     "gen-1",
				CreatedAt:        now,
			},
			{
				IntentID:         "ready-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->ready",
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-ready",
				RepositoryID:     "repo-ready",
				SourceRunID:      "run-2",
				GenerationID:     "gen-2",
				CreatedAt:        now.Add(time.Millisecond),
			},
		},
	}
	var acceptedPrefetchCalls [][]string
	var readinessPrefetchCalls [][]string
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGenPrefetch: func(_ context.Context, rows []SharedProjectionIntentRow) (AcceptedGenerationLookup, error) {
			acceptedPrefetchCalls = append(acceptedPrefetchCalls, acceptanceUnitIDs(rows))
			return func(key SharedProjectionAcceptanceKey) (string, bool) {
				switch key.AcceptanceUnitID {
				case "repo-blocked":
					return "gen-1", true
				case "repo-ready":
					return "gen-2", true
				default:
					return "", false
				}
			}, nil
		},
		ReadinessPrefetch: func(_ context.Context, keys []GraphProjectionPhaseKey, phase GraphProjectionPhase) (GraphProjectionReadinessLookup, error) {
			if phase != GraphProjectionPhaseCanonicalNodesCommitted {
				t.Fatalf("phase = %q, want %q", phase, GraphProjectionPhaseCanonicalNodesCommitted)
			}
			unitIDs := make([]string, 0, len(keys))
			for _, key := range keys {
				unitIDs = append(unitIDs, key.AcceptanceUnitID)
			}
			readinessPrefetchCalls = append(readinessPrefetchCalls, unitIDs)
			return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
				return key.AcceptanceUnitID == "repo-ready", true
			}, nil
		},
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          1,
			AcceptanceScanLimit: 10,
		},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got, want := key.AcceptanceUnitID, "repo-ready"; got != want {
		t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := acceptedPrefetchCalls, [][]string{{"repo-blocked"}, {"repo-ready"}}; !equalStringSlices(got, want) {
		t.Fatalf("acceptedPrefetchCalls = %v, want %v", got, want)
	}
	if got, want := readinessPrefetchCalls, [][]string{{"repo-blocked"}, {"repo-ready"}}; !equalStringSlices(got, want) {
		t.Fatalf("readinessPrefetchCalls = %v, want %v", got, want)
	}
}

func acceptanceUnitIDs(rows []SharedProjectionIntentRow) []string {
	unitIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		unitIDs = append(unitIDs, row.AcceptanceUnitID)
	}
	return unitIDs
}

func equalStringSlices(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !slices.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
