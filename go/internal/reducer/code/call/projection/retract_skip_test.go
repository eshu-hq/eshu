// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projection

import (
	"context"
	"testing"
	"time"

	codecall "github.com/eshu-hq/eshu/go/internal/reducer/code/call"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

func TestCodeCallProjectionRunnerSkipsRetractForDurableFirstProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 30, 0, 0, time.UTC)
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-1", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-1", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{fakeCodeCallIntentStore: baseReader}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 0; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d", got, want)
	}
	if got, want := len(writer.writeCalls), 1; got != want {
		t.Fatalf("len(writeCalls) = %d, want %d", got, want)
	}
	if got, want := result.RetractedRows, 0; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
	if got, want := result.UpsertedRows, 1; got != want {
		t.Fatalf("UpsertedRows = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerRetractsWhenDurableHistoryExists(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 35, 0, 0, time.UTC)
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-1", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-1", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: baseReader,
		hasCompleted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 2; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d", got, want)
	}
	if got, want := result.RetractedRows, 1; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerSkipsRetractForCurrentRunChunkAfterFirstChunk(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 37, 0, 0, time.UTC)
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{
			codeCallProjectionTestRow("edge-2", "gen-1", now),
		},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {
				codeCallProjectionTestRow("edge-2", "gen-1", now),
			},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: baseReader,
		hasCompleted:            true,
		hasCompletedCurrentRun:  true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 0; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d", got, want)
	}
	if got, want := result.UpsertedRows, 1; got != want {
		t.Fatalf("UpsertedRows = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerRetractsForDifferentCurrentRunPartition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 38, 0, 0, time.UTC)
	completedPartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	activePartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"src/models.go"})
	active := codeCallProjectionDeltaPartitionRow(
		"models-edge",
		activePartition,
		"repo-a",
		"src/models.go",
		now,
	)
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{active},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {active},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore:       baseReader,
		hasCompleted:                  true,
		hasCompletedCurrentRun:        true,
		completedCurrentRunPartitions: map[string]bool{completedPartition: true},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 2; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d for different completed partition", got, want)
	}
	assertCodeCallRetractPath(t, writer.retractCalls, "src/models.go")
	if got, want := result.RetractedRows, 1; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerSkipsRetractAfterCompletedCoveringRefresh(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 39, 0, 0, time.UTC)
	activePartition := codecall.RefreshPartitionKeyForDelta("repo-a", []string{"src/models.go"})
	active := codeCallProjectionDeltaPartitionRow(
		"models-edge",
		activePartition,
		"repo-a",
		"/repo/src/models.go",
		now,
	)
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{active},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {active},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{
		fakeCodeCallIntentStore: baseReader,
		hasCompleted:            true,
		completedCurrentRunRefresh: map[string]bool{
			"/repo/src/models.go": true,
		},
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 0; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d after covering refresh", got, want)
	}
	if got, want := result.RetractedRows, 0; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
	if got, want := result.UpsertedRows, 1; got != want {
		t.Fatalf("UpsertedRows = %d, want %d", got, want)
	}
}

func TestCodeCallProjectionRunnerRetractsWhenStaleRowsExistWithoutDurableHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 28, 17, 40, 0, 0, time.UTC)
	active := codeCallProjectionTestRow("edge-1", "gen-1", now)
	stale := codeCallProjectionTestRow("stale-1", "gen-old", now.Add(-time.Second))
	baseReader := &fakeCodeCallIntentStore{
		pendingByDomain: []sharedintent.Row{stale, active},
		pendingByAcceptance: map[string][]sharedintent.Row{
			"scope-a|repo-a|run-1": {stale, active},
		},
		leaseGranted: true,
	}
	reader := &historyAwareCodeCallIntentStore{fakeCodeCallIntentStore: baseReader}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := Runner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen: func(key sharedintent.AcceptanceKey) (string, bool) {
			return "gen-1", key.ScopeID == "scope-a" && key.AcceptanceUnitID == "repo-a" && key.SourceRunID == "run-1"
		},
		Config: RunnerConfig{BatchLimit: 10},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got, want := len(writer.retractCalls), 2; got != want {
		t.Fatalf("len(retractCalls) = %d, want %d", got, want)
	}
}
