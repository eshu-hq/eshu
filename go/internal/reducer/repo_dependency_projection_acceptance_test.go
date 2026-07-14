// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestRepoDependencyProjectionRunnerProcessesSourceRepoOwnedAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 12, 0, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			repoDependencyIntentRow(
				"stale-1", "scope-a", repoID, repoID, "run-1", "gen-old", now,
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_old",
					"relationship_type": "DEPENDS_ON",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
			repoDependencyIntentRow(
				"active-1", "scope-b", repoID, repoID, "run-2", "gen-2", now.Add(time.Second),
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_target_1",
					"relationship_type": "DEPENDS_ON",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
			repoDependencyIntentRow(
				"active-2", "scope-c", repoID, repoID, "run-3", "gen-3", now.Add(2*time.Second),
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_target_2",
					"relationship_type": "DEPLOYS_FROM",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
			repoDependencyIntentRow(
				"other-1", "scope-d", "repository:r_repo_b", "repository:r_repo_b", "run-4", "gen-4", now.Add(3*time.Second),
				map[string]any{
					"repo_id":           "repository:r_repo_b",
					"target_repo_id":    "repository:r_target_3",
					"relationship_type": "DEPENDS_ON",
					"evidence_source":   crossRepoEvidenceSource,
				},
			),
		},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{
			repoID: {
				repoDependencyIntentRow(
					"stale-1", "scope-a", repoID, repoID, "run-1", "gen-old", now,
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_old",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
				repoDependencyIntentRow(
					"active-1", "scope-b", repoID, repoID, "run-2", "gen-2", now.Add(time.Second),
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_1",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
				repoDependencyIntentRow(
					"active-2", "scope-c", repoID, repoID, "run-3", "gen-3", now.Add(2*time.Second),
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_2",
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
			switch {
			case key.ScopeID == "scope-a" && key.AcceptanceUnitID == repoID && key.SourceRunID == "run-1":
				return "gen-current", true
			case key.ScopeID == "scope-b" && key.AcceptanceUnitID == repoID && key.SourceRunID == "run-2":
				return "gen-2", true
			case key.ScopeID == "scope-c" && key.AcceptanceUnitID == repoID && key.SourceRunID == "run-3":
				return "gen-3", true
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
	if !result.LeaseAcquired {
		t.Fatal("LeaseAcquired = false, want true")
	}
	if result.ProcessedIntents != 3 {
		t.Fatalf("ProcessedIntents = %d, want 3", result.ProcessedIntents)
	}
	if len(writer.retractCalls) != 1 {
		t.Fatalf("len(retractCalls) = %d, want 1", len(writer.retractCalls))
	}
	if got, want := writer.retractCalls[0].evidenceSource, crossRepoEvidenceSource; got != want {
		t.Fatalf("retract evidenceSource = %q, want %q", got, want)
	}
	if len(writer.retractCalls[0].rows) != 1 {
		t.Fatalf("len(retractCalls[0].rows) = %d, want 1", len(writer.retractCalls[0].rows))
	}
	if got, want := writer.retractCalls[0].rows[0].RepositoryID, repoID; got != want {
		t.Fatalf("retract repo = %q, want %q", got, want)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("len(writeCalls) = %d, want 1", len(writer.writeCalls))
	}
	if len(writer.writeCalls[0].rows) != 2 {
		t.Fatalf("len(writeCalls[0].rows) = %d, want 2", len(writer.writeCalls[0].rows))
	}
	if len(reader.marked) != 3 {
		t.Fatalf("len(marked) = %d, want 3", len(reader.marked))
	}
	if got := reader.acceptanceUnitRequests; len(got) != 1 || got[0] != repoID {
		t.Fatalf("acceptanceUnitRequests = %v, want [%q]", got, repoID)
	}
}

func TestRepoDependencyProjectionRunnerHeartbeatsLongGraphWrite(t *testing.T) {
	now := time.Date(2026, time.April, 30, 14, 45, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	releaseWrite := make(chan struct{})
	heartbeatObserved := make(chan struct{})
	var heartbeatOnce sync.Once
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			repoDependencyIntentRow(
				"active-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
				map[string]any{
					"repo_id":           repoID,
					"target_repo_id":    "repository:r_target",
					"relationship_type": "DEPLOYS_FROM",
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
						"target_repo_id":    "repository:r_target",
						"relationship_type": "DEPLOYS_FROM",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
			},
		},
		leaseGranted: true,
		leaseClaimHook: func(claims int) {
			if claims >= 2 {
				heartbeatOnce.Do(func() { close(heartbeatObserved) })
			}
		},
	}
	writer := &blockingCodeCallProjectionEdgeWriter{release: releaseWrite}
	runner := RepoDependencyProjectionRunner{
		IntentReader:       reader,
		LeaseManager:       reader,
		AcceptanceUnitGate: reader,
		EdgeWriter:         writer,
		AcceptedGen:        acceptedGenerationFixed("gen-1", true),
		Config:             RepoDependencyProjectionRunnerConfig{LeaseTTL: 30 * time.Millisecond},
	}

	resultCh := make(chan struct {
		result PartitionProcessResult
		err    error
	}, 1)
	go func() {
		result, err := runner.processOnce(context.Background(), now)
		resultCh <- struct {
			result PartitionProcessResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case <-heartbeatObserved:
	case <-time.After(time.Second):
		t.Fatal("repo dependency runner did not heartbeat while graph write was blocked")
	}
	close(releaseWrite)
	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatalf("processOnce() error = %v", got.err)
		}
		if got.result.ProcessedIntents != 1 {
			t.Fatalf("ProcessedIntents = %d, want 1", got.result.ProcessedIntents)
		}
	case <-time.After(time.Second):
		t.Fatal("processOnce() did not return after graph write was released")
	}
	if got := reader.claimCount(); got < 2 {
		t.Fatalf("lease claims = %d, want initial claim plus heartbeat", got)
	}
}

func TestRepoDependencyProjectionRunnerRetractsPerEvidenceSourceAndSkipsRetractRowsOnWrite(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 12, 30, 0, 0, time.UTC)
	repoID := "repository:r_repo_a"
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain: []SharedProjectionIntentRow{
			repoDependencyIntentRow(
				"upsert-cross-repo", "scope-a", repoID, repoID, "run-1", "gen-1", now,
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
					"upsert-cross-repo", "scope-a", repoID, repoID, "run-1", "gen-1", now,
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_1",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   crossRepoEvidenceSource,
					},
				),
				repoDependencyIntentRow(
					"retract-finalization", "scope-b", repoID, repoID, "run-2", "gen-2", now.Add(time.Second),
					map[string]any{
						"repo_id":           repoID,
						"target_repo_id":    "repository:r_target_2",
						"relationship_type": "DEPENDS_ON",
						"evidence_source":   defaultEvidenceSource,
						"action":            "retract",
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
			case "run-1":
				return "gen-1", true
			case "run-2":
				return "gen-2", true
			default:
				return "", false
			}
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v", err)
	}
	if len(writer.retractCalls) != 2 {
		t.Fatalf("len(retractCalls) = %d, want 2 evidence-source retracts", len(writer.retractCalls))
	}
	gotSources := []string{writer.retractCalls[0].evidenceSource, writer.retractCalls[1].evidenceSource}
	sort.Strings(gotSources)
	wantSources := []string{crossRepoEvidenceSource, defaultEvidenceSource}
	sort.Strings(wantSources)
	if got, want := gotSources, wantSources; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("retract sources = %v, want %v", gotSources, wantSources)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("len(writeCalls) = %d, want 1", len(writer.writeCalls))
	}
	if len(writer.writeCalls[0].rows) != 1 {
		t.Fatalf("len(writeCalls[0].rows) = %d, want 1 active upsert row", len(writer.writeCalls[0].rows))
	}
	if got, want := writer.writeCalls[0].rows[0].IntentID, "upsert-cross-repo"; got != want {
		t.Fatalf("written intent = %q, want %q", got, want)
	}
}

func TestRepoDependencyProjectionRunnerRehydratesCompletedContributorRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 19, 12, 45, 0, 0, time.UTC)
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

	_, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if len(writer.writeCalls) != 1 {
		t.Fatalf("len(writeCalls) = %d, want 1", len(writer.writeCalls))
	}
	if len(writer.writeCalls[0].rows) != 2 {
		t.Fatalf("len(writeCalls[0].rows) = %d, want 2 to preserve completed contributor", len(writer.writeCalls[0].rows))
	}
}
