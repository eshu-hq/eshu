// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRepoDependencyProjectionRunnerDefersRunsOnUntilWorkloadReady(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 15, 16, 0, 0, 0, time.UTC)
	repoID := "repo-source"
	runsOn := repoDependencyIntentRow(
		"runs-on-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
		map[string]any{
			"repo_id":           repoID,
			"platform_id":       "platform:kubernetes:none:cluster/prod:none:none",
			"relationship_type": "RUNS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
	sibling := repoDependencyIntentRow(
		"depends-on-1", "scope-a", repoID, repoID, "run-1", "gen-1", now.Add(time.Millisecond),
		map[string]any{
			"repo_id":           repoID,
			"target_repo_id":    "repo-target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
	retractOnly := repoDependencyIntentRow(
		"retract-1", "scope-a", repoID, repoID, "run-1", "gen-1", now.Add(2*time.Millisecond),
		map[string]any{
			"action":          "retract",
			"repo_id":         repoID,
			"evidence_source": CrossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{runsOn},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {runsOn, sibling, retractOnly}},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	replayer := &recordingWorkloadMaterializationReplayer{}
	workloadReady := false
	fenceRequests, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{runsOn})
	if err != nil || len(fenceRequests) != 1 {
		t.Fatalf("build fence requests = (%d, %v), want (1, nil)", len(fenceRequests), err)
	}
	wantReadinessKey := fenceRequests[0].readinessKey
	legacyReadinessKey := workloadMaterializationRepoReadinessKey("scope-a", repoID, "gen-1")
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: replayer,
		WorkloadReadinessPrefetch: func(
			_ context.Context,
			keys []GraphProjectionPhaseKey,
			phase GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			if got, want := phase, GraphProjectionPhaseWorkloadMaterialization; got != want {
				t.Fatalf("readiness phase = %q, want %q", got, want)
			}
			if got, want := keys, []GraphProjectionPhaseKey{wantReadinessKey}; !reflect.DeepEqual(got, want) {
				t.Fatalf("readiness keys = %#v, want %#v", got, want)
			}
			return func(key GraphProjectionPhaseKey, _ GraphProjectionPhase) (bool, bool) {
				if key == legacyReadinessKey {
					return true, true
				}
				return workloadReady && key == wantReadinessKey, key == wantReadinessKey
			}, nil
		},
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			return "gen-1", key.AcceptanceUnitID == repoID
		},
		Config: RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	result, err := runner.processOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("processOnce() error = %v, want nil", err)
	}
	if got, want := result.BlockedReadiness, 1; got != want {
		t.Fatalf("BlockedReadiness = %d, want %d", got, want)
	}
	if got := len(writer.writeCalls); got != 0 {
		t.Fatalf("write calls = %d, want 0 before workload readiness", got)
	}
	if got := len(writer.retractCalls); got != 0 {
		t.Fatalf("retract calls = %d, want 0 before workload readiness", got)
	}
	if got, want := len(replayer.calls), 1; got != want {
		t.Fatalf("replayer calls = %d, want %d", got, want)
	}
	if call := replayer.calls[0]; !call.fenced || call.fence != fenceRequests[0].fence || call.repoID != repoID {
		t.Fatalf("replayer call = %#v, want fenced request %#v", call, fenceRequests[0])
	}
	if got := len(reader.marked); got != 0 {
		t.Fatalf("marked intents = %d, want 0 before workload readiness", got)
	}

	workloadReady = true
	result, err = runner.processOnce(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatalf("processOnce() after readiness error = %v, want nil", err)
	}
	if got := result.BlockedReadiness; got != 0 {
		t.Fatalf("BlockedReadiness after readiness = %d, want 0", got)
	}
	if got, want := len(writer.writeCalls), 1; got != want {
		t.Fatalf("write calls after readiness = %d, want %d", got, want)
	}
	if got, want := len(writer.writeCalls[0].rows), 2; got != want {
		t.Fatalf("written rows after readiness = %d, want %d", got, want)
	}
	if got, want := len(writer.retractCalls), 1; got != want {
		t.Fatalf("retract calls after readiness = %d, want %d", got, want)
	}
	if got, want := len(reader.marked), 3; got != want {
		t.Fatalf("marked intents after readiness = %d, want %d", got, want)
	}
}

func TestRepoDependencyRunsOnFenceIsOrderIndependentAndInputSensitive(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 15, 16, 15, 0, 0, time.UTC)
	rowA := repoDependencyIntentRow(
		"runs-on-a", "scope-a", "repo-a", "repo-a", "run-1", "gen-1", now,
		map[string]any{"repo_id": "repo-a", "relationship_type": "RUNS_ON"},
	)
	rowB := repoDependencyIntentRow(
		"runs-on-b", "scope-a", "repo-a", "repo-a", "run-1", "gen-1", now,
		map[string]any{"repo_id": "repo-a", "relationship_type": "RUNS_ON"},
	)

	ab, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{rowA, rowB})
	if err != nil || len(ab) != 1 {
		t.Fatalf("AB fence requests = (%d, %v), want (1, nil)", len(ab), err)
	}
	ba, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{rowB, rowA})
	if err != nil || len(ba) != 1 {
		t.Fatalf("BA fence requests = (%d, %v), want (1, nil)", len(ba), err)
	}
	a, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{rowA})
	if err != nil || len(a) != 1 {
		t.Fatalf("A fence requests = (%d, %v), want (1, nil)", len(a), err)
	}
	if ab[0].fence != ba[0].fence {
		t.Fatalf("row-order fences differ: AB=%q BA=%q", ab[0].fence, ba[0].fence)
	}
	if ab[0].fence == a[0].fence {
		t.Fatalf("input-set fences match: AB=%q A=%q", ab[0].fence, a[0].fence)
	}
	rowANewer := rowA
	rowANewer.CreatedAt = rowA.CreatedAt.Add(time.Nanosecond)
	newer, err := repoDependencyRunsOnFenceRequests([]SharedProjectionIntentRow{rowANewer})
	if err != nil || len(newer) != 1 {
		t.Fatalf("newer fence requests = (%d, %v), want (1, nil)", len(newer), err)
	}
	if a[0].fence == newer[0].fence {
		t.Fatalf("acceptance-epoch fences match: old=%q newer=%q", a[0].fence, newer[0].fence)
	}
}

func TestRepoDependencyProjectionRunnerRejectsUnschedulableRunsOnReplay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.September, 15, 16, 30, 0, 0, time.UTC)
	repoID := "repo-source"
	runsOn := repoDependencyIntentRow(
		"runs-on-1", "scope-a", repoID, repoID, "run-1", "gen-1", now,
		map[string]any{
			"repo_id":           repoID,
			"platform_id":       "platform:kubernetes:none:cluster/prod:none:none",
			"relationship_type": "RUNS_ON",
			"evidence_source":   CrossRepoEvidenceSource,
		},
	)
	reader := &fakeRepoDependencyIntentStore{
		pendingByDomain:         []SharedProjectionIntentRow{runsOn},
		pendingByAcceptanceUnit: map[string][]SharedProjectionIntentRow{repoID: {runsOn}},
		leaseGranted:            true,
	}
	writer := &recordingCodeCallProjectionEdgeWriter{}
	runner := RepoDependencyProjectionRunner{
		IntentReader:                    reader,
		LeaseManager:                    reader,
		AcceptanceUnitGate:              reader,
		EdgeWriter:                      writer,
		WorkloadMaterializationReplayer: &recordingWorkloadMaterializationReplayer{reject: true},
		WorkloadReadinessPrefetch: func(
			context.Context,
			[]GraphProjectionPhaseKey,
			GraphProjectionPhase,
		) (GraphProjectionReadinessLookup, error) {
			return func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
				return false, false
			}, nil
		},
		AcceptedGen: acceptedGenerationFixed("gen-1", true),
		Config:      RepoDependencyProjectionRunnerConfig{PollInterval: 10 * time.Millisecond},
	}

	_, err := runner.processOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "replay was not scheduled") {
		t.Fatalf("processOnce() error = %v, want unscheduled replay failure", err)
	}
	if got := len(writer.retractCalls) + len(writer.writeCalls) + len(reader.marked); got != 0 {
		t.Fatalf("graph mutations plus completed intents = %d, want 0 after unscheduled replay", got)
	}
}
