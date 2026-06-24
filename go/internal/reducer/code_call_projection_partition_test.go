// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestCodeCallProjectionRunnerProcessesDistinctDeltaFilePartitionsSeparately(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 10, 0, 0, 0, time.UTC)
	partitionCount := 16
	callerPartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	modelsPartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/models.go"})
	callerPartitionID := mustPartitionForKey(t, callerPartition, partitionCount)
	modelsPartitionID := mustPartitionForKey(t, modelsPartition, partitionCount)
	if callerPartitionID == modelsPartitionID {
		t.Fatalf("test partition keys mapped to same partition %d; choose different fixture paths", callerPartitionID)
	}

	callerRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		callerPartition,
		"repo-a",
		"/repo/src/caller.go",
		now,
	)
	modelsRow := codeCallProjectionDeltaPartitionRow(
		"models-edge",
		modelsPartition,
		"repo-a",
		"/repo/src/models.go",
		now.Add(time.Millisecond),
	)
	if !codeCallProjectionPartitionMatches(callerRow, callerPartitionID, partitionCount) {
		t.Fatalf("caller row does not match partition %d/%d", callerPartitionID, partitionCount)
	}
	if codeCallProjectionRowBlockedByRepoFence(callerRow, []SharedProjectionIntentRow{callerRow, modelsRow}, 0) {
		t.Fatal("caller row unexpectedly blocked by repo fence")
	}
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{callerRow, modelsRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {callerRow, modelsRow},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config: CodeCallProjectionRunnerConfig{
			BatchLimit:     10,
			PartitionCount: partitionCount,
			Workers:        2,
		},
	}
	selection, err := runner.selectAcceptanceUnitPartitionWorkWithStats(
		context.Background(),
		now,
		callerPartitionID,
		partitionCount,
	)
	if err != nil {
		t.Fatalf("select caller partition error = %v", err)
	}
	if selection.Key == (SharedProjectionAcceptanceKey{}) {
		t.Fatalf("selection = %#v, want caller partition key %q", selection, callerPartition)
	}
	if selection.PartitionKey != callerPartition {
		t.Fatalf("selection.PartitionKey = %q, want %q", selection.PartitionKey, callerPartition)
	}

	first, err := runner.processPartitionOnce(context.Background(), now, callerPartitionID, partitionCount)
	if err != nil {
		t.Fatalf("processPartitionOnce(caller) error = %v", err)
	}
	if got, want := first.ProcessedIntents, 1; got != want {
		t.Fatalf("caller ProcessedIntents = %d, want %d", got, want)
	}
	if !slices.Equal(reader.marked, []string{"caller-edge"}) {
		t.Fatalf("marked after caller partition = %v, want [caller-edge]", reader.marked)
	}
	assertCodeCallRetractPath(t, writer.retractCalls, "/repo/src/caller.go")

	second, err := runner.processPartitionOnce(context.Background(), now, modelsPartitionID, partitionCount)
	if err != nil {
		t.Fatalf("processPartitionOnce(models) error = %v", err)
	}
	if got, want := second.ProcessedIntents, 1; got != want {
		t.Fatalf("models ProcessedIntents = %d, want %d", got, want)
	}
	if !slices.Equal(reader.marked, []string{"caller-edge", "models-edge"}) {
		t.Fatalf("marked after both partitions = %v, want caller then models", reader.marked)
	}
	assertCodeCallRetractPath(t, writer.retractCalls[2:], "/repo/src/models.go")
}

func TestCodeCallProjectionRunnerWholeScopeBlocksFilePartitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 11, 0, 0, 0, time.UTC)
	partitionCount := 8
	wholeRow := codeCallProjectionWholeScopeRow("whole-refresh", "repo-a", now)
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now.Add(time.Millisecond),
	)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{wholeRow, fileRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {wholeRow, fileRow},
		},
		leaseGranted: true,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       CodeCallProjectionRunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
	}

	result, err := runner.processPartitionOnce(
		context.Background(),
		now,
		mustPartitionForKey(t, filePartition, partitionCount),
		partitionCount,
	)
	if err != nil {
		t.Fatalf("processPartitionOnce(file) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d, want file partition blocked by earlier whole scope", result.ProcessedIntents)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want none while whole scope fences file partitions", reader.marked)
	}
}

func TestCodeCallProjectionRunnerFileRefreshBlocksCoveredFilePartitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 11, 15, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	refreshPartition := codeCallRefreshPartitionKeyForDelta(
		"repo-a",
		[]string{"src/caller.go", "src/models.go"},
	)
	refreshRow := codeCallProjectionFileRefreshRow(
		"file-refresh",
		refreshPartition,
		"repo-a",
		[]string{"/repo/src/caller.go", "/repo/src/models.go"},
		now,
	)
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now.Add(time.Millisecond),
	)
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{fileRow, refreshRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {fileRow, refreshRow},
		},
		leaseGranted: true,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       CodeCallProjectionRunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
	}

	result, err := runner.processPartitionOnce(
		context.Background(),
		now,
		mustPartitionForKey(t, filePartition, partitionCount),
		partitionCount,
	)
	if err != nil {
		t.Fatalf("processPartitionOnce(file) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d, want file partition blocked by covering refresh", result.ProcessedIntents)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want none while refresh fences file partitions", reader.marked)
	}
}

func TestCodeCallProjectionRunnerFilePartitionsBlockLaterWholeScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 11, 30, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now,
	)
	wholeRow := codeCallProjectionWholeScopeRow("whole-refresh", "repo-a", now.Add(time.Millisecond))
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{fileRow, wholeRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {fileRow, wholeRow},
		},
		leaseGranted: true,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       CodeCallProjectionRunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
	}

	result, err := runner.processPartitionOnce(
		context.Background(),
		now,
		mustPartitionForKey(t, wholeRow.PartitionKey, partitionCount),
		partitionCount,
	)
	if err != nil {
		t.Fatalf("processPartitionOnce(whole) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d, want whole scope blocked by earlier file partition", result.ProcessedIntents)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want none while earlier file partition is pending", reader.marked)
	}
}

func TestCodeCallProjectionRunnerLaterWholeRefreshDoesNotBlockEarlierFilePartition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 11, 40, 0, 0, time.UTC)
	partitionCount := 8
	filePartition := codeCallRefreshPartitionKeyForDelta("repo-a", []string{"src/caller.go"})
	fileRow := codeCallProjectionDeltaPartitionRow(
		"caller-edge",
		filePartition,
		"repo-a",
		"/repo/src/caller.go",
		now,
	)
	wholeRow := codeCallProjectionWholeScopeRow("whole-refresh", "repo-a", now.Add(time.Millisecond))
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{fileRow, wholeRow},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {fileRow, wholeRow},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   writer,
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       CodeCallProjectionRunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
	}

	result, err := runner.processPartitionOnce(
		context.Background(),
		now,
		mustPartitionForKey(t, filePartition, partitionCount),
		partitionCount,
	)
	if err != nil {
		t.Fatalf("processPartitionOnce(file) error = %v", err)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want earlier file partition to run before later whole refresh", got)
	}
	if !slices.Equal(reader.marked, []string{"caller-edge"}) {
		t.Fatalf("marked = %v, want only earlier file partition completed", reader.marked)
	}
	assertCodeCallRetractPath(t, writer.retractCalls, "/repo/src/caller.go")
}

func TestCodeCallProjectionRunnerWholeScopeBlocksLaterWholeScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 15, 11, 45, 0, 0, time.UTC)
	partitionCount := 8
	firstWhole := codeCallProjectionLegacyWholeScopeRow("whole-a", "legacy:a", "repo-a", now)
	secondWhole := codeCallProjectionLegacyWholeScopeRow("whole-b", "legacy:b", "repo-a", now.Add(time.Millisecond))
	firstPartition := mustPartitionForKey(t, firstWhole.PartitionKey, partitionCount)
	secondPartition := mustPartitionForKey(t, secondWhole.PartitionKey, partitionCount)
	if firstPartition == secondPartition {
		t.Fatalf("test legacy whole keys mapped to same partition %d; choose different keys", firstPartition)
	}
	reader := &fakeCodeCallIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{firstWhole, secondWhole},
		pendingByAcceptance: map[string][]SharedProjectionIntentRow{
			"scope-a|repo-a|run-1": {firstWhole, secondWhole},
		},
		leaseGranted: true,
	}
	runner := CodeCallProjectionRunner{
		IntentReader: reader,
		LeaseManager: reader,
		EdgeWriter:   &recordingCodeCallProjectionEdgeWriter{},
		AcceptedGen:  acceptedGenerationFixed("gen-1", true),
		Config:       CodeCallProjectionRunnerConfig{BatchLimit: 10, PartitionCount: partitionCount},
	}

	result, err := runner.processPartitionOnce(context.Background(), now, secondPartition, partitionCount)
	if err != nil {
		t.Fatalf("processPartitionOnce(second whole) error = %v", err)
	}
	if result.ProcessedIntents != 0 {
		t.Fatalf("ProcessedIntents = %d, want later whole scope blocked by earlier whole scope", result.ProcessedIntents)
	}
	if len(reader.marked) != 0 {
		t.Fatalf("marked = %v, want none while earlier whole scope fences later whole scope", reader.marked)
	}
}

func mustPartitionForKey(t *testing.T, partitionKey string, partitionCount int) int {
	t.Helper()

	partitionID, err := PartitionForKey(partitionKey, partitionCount)
	if err != nil {
		t.Fatalf("PartitionForKey(%q, %d) error = %v", partitionKey, partitionCount, err)
	}
	return partitionID
}

func codeCallProjectionDeltaPartitionRow(
	intentID string,
	partitionKey string,
	repositoryID string,
	deltaFilePath string,
	createdAt time.Time,
) SharedProjectionIntentRow {
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     partitionKey,
		ScopeID:          "scope-a",
		AcceptanceUnitID: repositoryID,
		RepositoryID:     repositoryID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"repo_id":          repositoryID,
			"caller_entity_id": "caller:" + intentID,
			"callee_entity_id": "callee:" + intentID,
			"evidence_source":  codeCallEvidenceSource,
			"delta_projection": true,
			"delta_file_paths": []string{deltaFilePath},
		},
		CreatedAt: createdAt,
	}
}

func codeCallProjectionWholeScopeRow(intentID string, repositoryID string, createdAt time.Time) SharedProjectionIntentRow {
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     codeCallWholeScopePartitionKey(repositoryID),
		ScopeID:          "scope-a",
		AcceptanceUnitID: repositoryID,
		RepositoryID:     repositoryID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"repo_id":         repositoryID,
			"action":          "refresh",
			"intent_type":     "repo_refresh",
			"evidence_source": codeCallRepoRefreshEvidenceSource,
		},
		CreatedAt: createdAt,
	}
}

func codeCallProjectionFileRefreshRow(
	intentID string,
	partitionKey string,
	repositoryID string,
	deltaFilePaths []string,
	createdAt time.Time,
) SharedProjectionIntentRow {
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainCodeCalls,
		PartitionKey:     partitionKey,
		ScopeID:          "scope-a",
		AcceptanceUnitID: repositoryID,
		RepositoryID:     repositoryID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"repo_id":          repositoryID,
			"action":           "refresh",
			"intent_type":      "repo_refresh",
			"evidence_source":  codeCallRepoRefreshEvidenceSource,
			"delta_projection": true,
			"delta_file_paths": append([]string(nil), deltaFilePaths...),
		},
		CreatedAt: createdAt,
	}
}

func codeCallProjectionLegacyWholeScopeRow(
	intentID string,
	partitionKey string,
	repositoryID string,
	createdAt time.Time,
) SharedProjectionIntentRow {
	row := codeCallProjectionWholeScopeRow(intentID, repositoryID, createdAt)
	row.PartitionKey = partitionKey
	return row
}

func assertCodeCallRetractPath(t *testing.T, calls []recordedProjectionCall, wantPath string) {
	t.Helper()

	if len(calls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want two evidence-source retracts", len(calls))
	}
	for i, call := range calls {
		if len(call.rows) != 1 {
			t.Fatalf("retractCalls[%d].rows len = %d, want 1", i, len(call.rows))
		}
		got := semanticPayloadStringSlice(call.rows[0].Payload, "delta_file_paths")
		if !slices.Equal(got, []string{wantPath}) {
			t.Fatalf("retractCalls[%d] delta_file_paths = %v, want [%s]", i, got, wantPath)
		}
	}
}
