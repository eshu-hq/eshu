// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestRepoDependencyProjectionRunnerSkipsRetractWhenCompletedContributorsRemainAuthoritative(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 29, 15, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	completedAt := now.Add(-time.Minute)
	completedContributor := repoDependencyIntentRow(
		"completed-1", "scope-old", repoID, repoID, "run-old", "gen-old", now.Add(-2*time.Minute),
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repository:r_target_old",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
	completedContributor.CompletedAt = &completedAt

	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			repoDependencyIntentRow(
				"pending-1", "scope-new", repoID, repoID, "run-new", "gen-new", now,
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_target_new",
					"relationship_type": "DEPLOYS_FROM",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
		},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {
				completedContributor,
				repoDependencyIntentRow(
					"pending-1", "scope-new", repoID, repoID, "run-new", "gen-new", now,
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_new",
						"relationship_type": "DEPLOYS_FROM",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
			},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader:       reader,
		LeaseManager:       reader,
		AcceptanceUnitGate: reader,
		EdgeWriter:         writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			switch key.SourceRunID {
			case "run-old":
				return "gen-old", true
			case "run-new":
				return "gen-new", true
			default:
				return "", false
			}
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got := len(writer.retractCalls); got != 0 {
		t.Fatalf("retract calls = %d, want 0 when completed contributors remain authoritative", got)
	}
	if got := len(writer.writeCalls); got != 1 {
		t.Fatalf("write calls = %d, want 1", got)
	}
	if got, want := len(writer.writeCalls[0].rows), 2; got != want {
		t.Fatalf("written rows = %d, want %d", got, want)
	}
	if got, want := result.ProcessedIntents, 2; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
}

func TestRepoDependencyProjectionRunnerReplaysWorkloadMaterializationForActiveRepoDependencyWrites(t *testing.T) {
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
				repoDependencyIntentRow(
					"active-2", "scope-a", repoID, repoID, "run-2", "gen-1", now.Add(time.Second),
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_2",
						"relationship_type": "DEPLOYS_FROM",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
				repoDependencyIntentRow(
					"active-3", "scope-b", repoID, repoID, "run-3", "gen-2", now.Add(2*time.Second),
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_3",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   defaultEvidenceSource,
					},
				),
			},
		},
		leaseGranted: true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	replayer := &recordingWorkloadMaterializationReplayer{}
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			switch key.SourceRunID {
			case "run-1", "run-2":
				return "gen-1", true
			case "run-3":
				return "gen-2", true
			default:
				return "", false
			}
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if got, want := len(replayer.calls), 2; got != want {
		t.Fatalf("replayer calls = %d, want %d", got, want)
	}
	if got, want := replayer.calls[0].scopeID, "scope-a"; got != want {
		t.Fatalf("replayer calls[0].scopeID = %q, want %q", got, want)
	}
	if got, want := replayer.calls[0].generationID, "gen-1"; got != want {
		t.Fatalf("replayer calls[0].generationID = %q, want %q", got, want)
	}
	if got, want := replayer.calls[0].entityKey, "repo:r_repo_a"; got != want {
		t.Fatalf("replayer calls[0].entityKey = %q, want %q", got, want)
	}
	if got, want := replayer.calls[1].scopeID, "scope-b"; got != want {
		t.Fatalf("replayer calls[1].scopeID = %q, want %q", got, want)
	}
	if got, want := replayer.calls[1].generationID, "gen-2"; got != want {
		t.Fatalf("replayer calls[1].generationID = %q, want %q", got, want)
	}
	if got, want := replayer.calls[1].entityKey, "repo:r_repo_a"; got != want {
		t.Fatalf("replayer calls[1].entityKey = %q, want %q", got, want)
	}
}

func TestRepoDependencyReplayRequestsUseTargetForProvisionedDependencies(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		{
			ScopeID:      "git-repository-scope:repository:r_service",
			GenerationID: "gen-service",
			RepositoryID: "repository:r_infra",
			Payload: map[string]any{
				"repo_id":           "repository:r_infra",
				"target_repo_id":    "repository:r_service",
				"relationship_type": "PROVISIONS_DEPENDENCY_FOR",
			},
		},
	}

	requests := repoDependencyReplayRequests(rows)
	if got, want := len(requests), 1; got != want {
		t.Fatalf("len(requests) = %d, want %d", got, want)
	}
	if got, want := requests[0].entityKey, "repo:r_service"; got != want {
		t.Fatalf("requests[0].entityKey = %q, want %q", got, want)
	}
}

func TestRepoDependencyProjectionRunnerSkipsRetractForFirstProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 29, 14, 0, 0, 0, time.UTC)
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
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader:       reader,
		LeaseManager:       reader,
		AcceptanceUnitGate: reader,
		EdgeWriter:         writer,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.AcceptanceUnitID == repoID
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if got := len(writer.retractCalls); got != 0 {
		t.Fatalf("retract calls = %d, want 0 for first projection", got)
	}
	if got := len(writer.writeCalls); got != 1 {
		t.Fatalf("write calls = %d, want 1", got)
	}
	if got, want := result.RetractedRows, 0; got != want {
		t.Fatalf("RetractedRows = %d, want %d", got, want)
	}
	if got, want := result.ProcessedIntents, 1; got != want {
		t.Fatalf("ProcessedIntents = %d, want %d", got, want)
	}
}

func TestRepoDependencyProjectionRunnerRecordCycleLogsSubstepDurations(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bootstrap, err := telemetry.NewBootstrap("test-reducer")
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}
	logger := telemetry.NewLoggerWithWriter(bootstrap, "reducer", "reducer", &buf)
	runner := RepoDependencyProjectionRunner{Logger: logger}

	runner.recordRepoDependencyCycle(
		context.Background(),
		"repository:r_repo_a",
		nil,
		2,
		1,
		time.Now().Add(-500*time.Millisecond),
		PartitionProcessResult{
			ProcessedIntents:                  3,
			StaleIntents:                      1,
			SelectionDurationSeconds:          0.05,
			LoadAllDurationSeconds:            0.07,
			AcceptancePrefetchDurationSeconds: 0.03,
			RetractDurationSeconds:            0.11,
			WriteDurationSeconds:              0.16,
			ReplayDurationSeconds:             0.04,
			MarkCompletedDurationSeconds:      0.02,
			ActiveIntents:                     2,
			ReplayRequests:                    1,
			AcceptanceUnitRows:                3,
		},
	)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	assertFloatLogValue(t, entry, "selection_duration_seconds", 0.05)
	assertFloatLogValue(t, entry, "load_all_duration_seconds", 0.07)
	assertFloatLogValue(t, entry, "acceptance_prefetch_duration_seconds", 0.03)
	assertFloatLogValue(t, entry, "retract_duration_seconds", 0.11)
	assertFloatLogValue(t, entry, "write_duration_seconds", 0.16)
	assertFloatLogValue(t, entry, "replay_duration_seconds", 0.04)
	assertFloatLogValue(t, entry, "mark_completed_duration_seconds", 0.02)
	assertFloatLogValue(t, entry, "processed_intents", 3)
	assertFloatLogValue(t, entry, "active_intents", 2)
	assertFloatLogValue(t, entry, "stale_intents", 1)
	assertFloatLogValue(t, entry, "acceptance_unit_rows", 3)
	assertFloatLogValue(t, entry, "replay_requests", 1)
}

func assertFloatLogValue(t *testing.T, entry map[string]any, key string, want float64) {
	t.Helper()

	got, ok := entry[key]
	if !ok {
		t.Fatalf("missing log key %q in entry %v", key, entry)
	}
	if got != want {
		t.Fatalf("%s = %v, want %v", key, got, want)
	}
}
