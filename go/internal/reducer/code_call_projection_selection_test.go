// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerSelectsAcceptanceUnitUsingScopeAndUnit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "scope-b",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-b",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-b",
				CreatedAt:        now,
			},
			{
				IntentID:         "scope-a",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
				ScopeID:          "scope-a",
				AcceptanceUnitID: "repo-a",
				RepositoryID:     "repo-a",
				SourceRunID:      "run-1",
				GenerationID:     "gen-a",
				CreatedAt:        now.Add(time.Second),
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			if key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1" {
				return "gen-a", true
			}
			return "", false
		},
		Config: CodeCallProjectionRunnerConfig{BatchLimit: 10},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got, want := key.ScopeID, "scope-a"; got != want {
		t.Fatalf("key.ScopeID = %q, want %q", got, want)
	}
	if got, want := key.AcceptanceUnitID, "repo-a"; got != want {
		t.Fatalf("key.AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := key.SourceRunID, "run-1"; got != want {
		t.Fatalf("key.SourceRunID = %q, want %q", got, want)
	}
}

func TestCodeCallProjectionRunnerSelectsAcceptanceUnitBeyondInitialBatchWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 11, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "stale-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee-1",
				ScopeID:          "scope-stale-1",
				AcceptanceUnitID: "repo-stale-1",
				RepositoryID:     "repo-stale-1",
				SourceRunID:      "run-stale-1",
				GenerationID:     "gen-stale-1",
				CreatedAt:        now,
			},
			{
				IntentID:         "stale-2",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee-2",
				ScopeID:          "scope-stale-2",
				AcceptanceUnitID: "repo-stale-2",
				RepositoryID:     "repo-stale-2",
				SourceRunID:      "run-stale-2",
				GenerationID:     "gen-stale-2",
				CreatedAt:        now.Add(time.Second),
			},
			{
				IntentID:         "accepted-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee-3",
				ScopeID:          "scope-accepted",
				AcceptanceUnitID: "repo-accepted",
				RepositoryID:     "repo-accepted",
				SourceRunID:      "run-accepted",
				GenerationID:     "gen-accepted",
				CreatedAt:        now.Add(2 * time.Second),
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			if key.ScopeID == "scope-accepted" && key.AcceptanceUnitID == "repo-accepted" && key.SourceRunID == "run-accepted" {
				return "gen-accepted", true
			}
			return "", false
		},
		Config: CodeCallProjectionRunnerConfig{BatchLimit: 2},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got, want := key.ScopeID, "scope-accepted"; got != want {
		t.Fatalf("key.ScopeID = %q, want %q", got, want)
	}
	if got, want := key.AcceptanceUnitID, "repo-accepted"; got != want {
		t.Fatalf("key.AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := key.SourceRunID, "run-accepted"; got != want {
		t.Fatalf("key.SourceRunID = %q, want %q", got, want)
	}
	if len(reader.domainLimitRequests) < 2 {
		t.Fatalf("domainLimitRequests = %v, want widened scan window", reader.domainLimitRequests)
	}
}

func TestCodeCallProjectionRunnerScansAcceptanceUnitForCoveringRefreshBeyondDomainPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 11, 30, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	refreshPartition := codeCallRefreshPartitionKeyForDelta(
		"repo-a",
		[]string{"src/caller.go", "src/models.go"},
	)
	filePartitionID := mustPartitionForKey(t, filePartition, partitionCount)
	if got := mustPartitionForKey(t, refreshPartition, partitionCount); got == filePartitionID {
		t.Fatalf("refresh partition mapped to file partition %d; choose different fixture paths", filePartitionID)
	}
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now,
	)
	refreshRow := codeCallProjectionFileRefreshRow(
		"covering-refresh",
		refreshPartition,
		"repo-a",
		[]string{"/repo/src/caller.go", "/repo/src/models.go"},
		now.Add(time.Millisecond),
	)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{fileRow, refreshRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {fileRow, refreshRow},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          1,
			AcceptanceScanLimit: 10,
			PartitionCount:      partitionCount,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		filePartitionID,
		partitionCount,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if result.Key != (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("selection = %#v, want file partition blocked by covering refresh outside initial page", result)
	}
	if got, want := reader.domainLimitRequests[0], 1; got != want {
		t.Fatalf("first domain limit = %d, want %d", got, want)
	}
	if len(reader.acceptanceLimitRequests) == 0 {
		t.Fatal("acceptanceLimitRequests empty, want acceptance-unit scan for covering refresh")
	}
}

func TestCodeCallProjectionRunnerUsesBoundedRefreshFenceLookup(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 19, 9, 0, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	refreshPartition := codeCallRefreshPartitionKeyForDelta(
		"repo-a",
		[]string{"src/caller.go", "src/models.go"},
	)
	filePartitionID := mustPartitionForKey(t, filePartition, partitionCount)
	if got := mustPartitionForKey(t, refreshPartition, partitionCount); got == filePartitionID {
		t.Fatalf("refresh partition mapped to file partition %d; choose different fixture paths", filePartitionID)
	}
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now,
	)
	refreshRow := codeCallProjectionFileRefreshRow(
		"covering-refresh",
		refreshPartition,
		"repo-a",
		[]string{"/repo/src/caller.go", "/repo/src/models.go"},
		now.Add(time.Millisecond),
	)
	base := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{fileRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {fileRow, refreshRow},
		},
	}
	reader := &fenceAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: base,
		blockedByFence:          true,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:          1,
			AcceptanceScanLimit: 10,
			PartitionCount:      partitionCount,
		},
	}

	result, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		filePartitionID,
		partitionCount,
	)
	if err != nil {
		t.Fatalf("selectAcceptanceUnitPartitionWorkWithStats() error = %v", err)
	}
	if result.Key != (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("selection = %#v, want file partition blocked by covering refresh", result)
	}
	if len(reader.checkedRows) == 0 {
		t.Fatal("checkedRows empty, want bounded fence lookup")
	}
	for _, checkedRow := range reader.checkedRows {
		if checkedRow != "caller-edge" {
			t.Fatalf("checkedRows = %v, want only caller-edge lookups", reader.checkedRows)
		}
	}
	if len(base.acceptanceLimitRequests) != 0 {
		t.Fatalf("acceptanceLimitRequests = %v, want no full acceptance-unit load", base.acceptanceLimitRequests)
	}
}

func TestCodeCallProjectionRunnerSkipsAcceptanceUnitUntilCanonicalNodesCommitted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "accepted-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
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
		IntentReader:    reader,
		AcceptedGen:     acceptedGenerationFixed("gen-1", true),
		ReadinessLookup: readinessLookupFixed(false, false),
		Config:          CodeCallProjectionRunnerConfig{BatchLimit: 10},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if key != (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("key = %#v, want zero value while canonical node readiness is missing", key)
	}
}

func TestCodeCallProjectionRunnerUsesCanonicalNodeReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 12, 15, 0, 0, time.UTC)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			{
				IntentID:         "accepted-1",
				ProjectionDomain: DomainCodeCalls,
				PartitionKey:     "caller->callee",
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
		ReadinessLookup: func(_ GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
			return phase == GraphProjectionPhaseCanonicalNodesCommitted, true
		},
		Config: CodeCallProjectionRunnerConfig{BatchLimit: 10},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got, want := key.AcceptanceUnitID, "repo-a"; got != want {
		t.Fatalf("key.AcceptanceUnitID = %q, want %q", got, want)
	}
}

func TestCodeCallProjectionRunnerSelectsReadyAcceptanceUnitWhenEarlierUnitIsBlockedByReadiness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 17, 12, 30, 0, 0, time.UTC)
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
				CreatedAt:        now.Add(time.Second),
			},
		},
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			switch key.AcceptanceUnitID {
			case "repo-blocked":
				return "gen-1", true
			case "repo-ready":
				return "gen-2", true
			default:
				return "", false
			}
		},
		ReadinessLookup: func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool) {
			if phase != GraphProjectionPhaseCanonicalNodesCommitted {
				t.Fatalf("phase = %q, want %q", phase, GraphProjectionPhaseCanonicalNodesCommitted)
			}
			if key.AcceptanceUnitID == "repo-ready" {
				return true, true
			}
			return false, false
		},
		Config: CodeCallProjectionRunnerConfig{BatchLimit: 10},
	}

	key, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got, want := key.AcceptanceUnitID, "repo-ready"; got != want {
		t.Fatalf("key.AcceptanceUnitID = %q, want %q", got, want)
	}
	if got, want := key.ScopeID, "scope-b"; got != want {
		t.Fatalf("key.ScopeID = %q, want %q", got, want)
	}
	if got, want := key.SourceRunID, "run-2"; got != want {
		t.Fatalf("key.SourceRunID = %q, want %q", got, want)
	}
}
