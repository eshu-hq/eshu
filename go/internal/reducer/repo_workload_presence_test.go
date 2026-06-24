// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// runsInIntentRow builds a minimal DomainRunsIn intent row carrying the repo_id
// the repo-workload presence gate keys on.
func runsInIntentRow(intentID, repoID string) SharedProjectionIntentRow {
	now := time.Date(2026, time.June, 17, 12, 0, 0, 0, time.UTC)
	return SharedProjectionIntentRow{
		IntentID:         intentID,
		ProjectionDomain: DomainRunsIn,
		PartitionKey:     intentID,
		ScopeID:          "scope-a",
		AcceptanceUnitID: repoID,
		RepositoryID:     repoID,
		SourceRunID:      "run-1",
		GenerationID:     "gen-1",
		Payload: map[string]any{
			"function_id": "fn-" + intentID,
			"repo_id":     repoID,
		},
		CreatedAt: now,
	}
}

func TestRepoWorkloadPresenceKeyShape(t *testing.T) {
	t.Parallel()

	if got := repoWorkloadPresenceKey("repo-1"); got != "repo-1" {
		t.Fatalf("presence key = %q, want repo-1", got)
	}
	if got := repoWorkloadPresenceKey("  repo-2  "); got != "repo-2" {
		t.Fatalf("presence key = %q, want trimmed repo-2", got)
	}
	if got := repoWorkloadPresenceKey(""); got != "" {
		t.Fatalf("blank repo key = %q, want empty", got)
	}
}

// TestPublishRepoWorkloadPresenceDeduplicatesByRepo proves a repo with several
// committed Workloads collapses to a single repo-keyed presence row, so the
// batched ON CONFLICT (keyspace, uid) upsert never carries the same conflict key
// twice (#2855, same failure class as the endpoint dedupe).
func TestPublishRepoWorkloadPresenceDeduplicatesByRepo(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	rows := []WorkloadRow{
		{RepoID: "repo-1", WorkloadID: "wl-1"},
		{RepoID: "repo-1", WorkloadID: "wl-2"}, // same repo, different workload
		{RepoID: "repo-2", WorkloadID: "wl-3"},
		{RepoID: "", WorkloadID: "wl-blank"}, // blank repo skipped
	}
	if err := publishRepoWorkloadPresence(
		context.Background(), writer, "scope-1", "gen-1", rows, time.Unix(1700000000, 0).UTC(),
	); err != nil {
		t.Fatalf("publish error = %v", err)
	}
	if len(writer.upserts) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(writer.upserts))
	}
	got := writer.upserts[0]
	if len(got) != 2 {
		t.Fatalf("presence rows = %d, want 2 (repo-1 collapsed, blank skipped)", len(got))
	}
	for _, r := range got {
		if r.Keyspace != GraphProjectionKeyspaceRepoWorkloadPresence {
			t.Fatalf("keyspace = %q, want %q", r.Keyspace, GraphProjectionKeyspaceRepoWorkloadPresence)
		}
		if r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("malformed presence row: %+v", r)
		}
		// #2842 provenance: each row carries its repo_id (= uid here) and the
		// materializing generation so stale rows can be retracted per repo.
		if r.RepoID != r.UID || r.SourceGeneration != "gen-1" {
			t.Fatalf("row missing #2842 provenance: %+v", r)
		}
	}
	// #2842: after upsert, the stale other-generation rows for the materialized
	// repos are retracted (race-free: only OTHER generations).
	if len(writer.staleRetract) != 1 {
		t.Fatalf("stale-retract calls = %d, want 1", len(writer.staleRetract))
	}
	sr := writer.staleRetract[0]
	if sr.keyspace != GraphProjectionKeyspaceRepoWorkloadPresence || sr.scopeID != "scope-1" || sr.generationID != "gen-1" {
		t.Fatalf("stale-retract args = %+v, want repo_workload/scope-1/gen-1", sr)
	}
	if len(sr.repoIDs) != 2 {
		t.Fatalf("stale-retract repoIDs = %v, want the 2 materialized repos", sr.repoIDs)
	}
}

func TestPublishRepoWorkloadPresenceNilWriterNoOp(t *testing.T) {
	t.Parallel()

	if err := publishRepoWorkloadPresence(
		context.Background(), nil, "scope-1", "gen-1",
		[]WorkloadRow{{RepoID: "repo-1", WorkloadID: "wl-1"}},
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("nil writer error = %v, want nil", err)
	}
}

// TestFilterRowsByReadinessRunsInTerminatesAbsentWorkload proves a phase-ready
// runs_in row whose repo has no committed :Workload presence is TERMINAL (no
// edge), not deferred — mirroring the handles_route liveness contract (#2855).
func TestFilterRowsByReadinessRunsInTerminatesAbsentWorkload(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		runsInIntentRow("present", "repo-with-workload"),
		runsInIntentRow("absent", "repo-no-workload"),
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		repoWorkloadPresenceKey("repo-with-workload"): {},
	}}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainRunsIn, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 1 || ready[0].IntentID != "present" {
		t.Fatalf("ready = %+v, want only the repo-with-workload intent", ready)
	}
	if len(blocked) != 0 {
		t.Fatalf("blocked = %+v, want none (absent workload is terminal, not deferred)", blocked)
	}
	if len(terminal) != 1 || terminal[0].IntentID != "absent" {
		t.Fatalf("terminal = %+v, want only the no-workload intent", terminal)
	}
	if presence.calls == 0 {
		t.Fatal("presence lookup never queried for runs_in")
	}
}

// TestFilterRowsByReadinessRunsInProjectsWhenWorkloadPresent proves runs_in rows
// project once their repo's :Workload presence is committed.
func TestFilterRowsByReadinessRunsInProjectsWhenWorkloadPresent(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		runsInIntentRow("a", "repo-1"),
		runsInIntentRow("b", "repo-1"),
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		repoWorkloadPresenceKey("repo-1"): {},
	}}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainRunsIn, rows, phaseReady, nil, presence,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 2 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("ready=%d blocked=%d terminal=%d, want 2/0/0", len(ready), len(blocked), len(terminal))
	}
}

// TestFilterRowsByReadinessRunsInNilPresenceIsTodaysBehavior proves a nil
// presence lookup leaves runs_in byte-identical to its phase-gate-only behavior.
func TestFilterRowsByReadinessRunsInNilPresenceIsTodaysBehavior(t *testing.T) {
	t.Parallel()

	rows := []SharedProjectionIntentRow{
		runsInIntentRow("a", "repo-1"),
		runsInIntentRow("b", "repo-2"),
	}
	phaseReady := func(GraphProjectionPhaseKey, GraphProjectionPhase) (bool, bool) {
		return true, true
	}

	ready, blocked, terminal, err := filterRowsByReadiness(
		context.Background(), DomainRunsIn, rows, phaseReady, nil, nil,
	)
	if err != nil {
		t.Fatalf("filterRowsByReadiness error = %v", err)
	}
	if len(ready) != 2 || len(blocked) != 0 || len(terminal) != 0 {
		t.Fatalf("nil presence changed runs_in behavior: ready=%d blocked=%d terminal=%d, want 2/0/0", len(ready), len(blocked), len(terminal))
	}
}

// TestProcessPartitionOnceRunsInDrainsAbsentWorkload proves the terminal cycle
// for runs_in: a row whose repo has no Workload is drained (marked complete, no
// edge), reported as terminal, never deferred (#2855).
func TestProcessPartitionOnceRunsInDrainsAbsentWorkload(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			runsInIntentRow("present", "repo-1"),
			runsInIntentRow("absent", "repo-2"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		repoWorkloadPresenceKey("repo-1"): {},
	}}

	cfg := PartitionProcessorConfig{
		Domain:         DomainRunsIn,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: runsInEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, presence, nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (absent workload is terminal)", result.BlockedReadiness)
	}
	if result.TerminalNoEndpoint != 1 {
		t.Fatalf("TerminalNoEndpoint = %d, want 1 (the no-workload repo)", result.TerminalNoEndpoint)
	}
	completed := map[string]bool{}
	for _, id := range reader.completedIDs {
		completed[id] = true
	}
	if !completed["present"] || !completed["absent"] {
		t.Fatalf("completedIDs = %v, want both drained", reader.completedIDs)
	}
	var writtenIDs []string
	for _, batch := range edges.writeCalls {
		for _, row := range batch {
			writtenIDs = append(writtenIDs, row.IntentID)
		}
	}
	if len(writtenIDs) != 1 || writtenIDs[0] != "present" {
		t.Fatalf("written rows = %v, want only the repo-with-workload intent", writtenIDs)
	}
}

// TestWorkloadMaterializationHandlerPublishesRepoWorkloadPresence proves the
// workload handler publishes repo-keyed :Workload presence (#2855) under the
// repo-workload keyspace using the same EndpointPresenceWriter, so the runs_in
// gate can see committed workloads.
func TestWorkloadMaterializationHandlerPublishesRepoWorkloadPresence(t *testing.T) {
	t.Parallel()

	inputLoader := &stubWorkloadProjectionInputLoader{
		candidates: []WorkloadCandidate{
			{
				RepoID:         "repo-service-api",
				RepoName:       "service-api",
				Classification: "service",
				Confidence:     0.96,
				Provenance:     []string{"dockerfile_runtime"},
			},
		},
	}
	presenceWriter := &recordingPresenceWriter{}
	handler := WorkloadMaterializationHandler{
		FactLoader:             &stubFactLoader{},
		InputLoader:            inputLoader,
		Materializer:           NewWorkloadMaterializer(&recordingCypherExecutor{}),
		EndpointPresenceWriter: presenceWriter,
	}

	now := time.Now().UTC()
	intent := Intent{
		IntentID:        "intent-wm-repo-workload-presence",
		ScopeID:         "scope-service",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "workloads ready",
		EntityKeys:      []string{"repo-service-api"},
		RelatedScopeIDs: []string{"scope-service"},
		EnqueuedAt:      now,
		AvailableAt:     now,
		Status:          IntentStatusPending,
	}

	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	wantUID := repoWorkloadPresenceKey("repo-service-api")
	found := false
	for _, batch := range presenceWriter.upserts {
		for _, row := range batch {
			if row.Keyspace == GraphProjectionKeyspaceRepoWorkloadPresence && row.UID == wantUID {
				found = true
				if row.ScopeID != "scope-service" {
					t.Fatalf("presence scope = %q, want scope-service", row.ScopeID)
				}
			}
		}
	}
	if !found {
		t.Fatalf("no repo-workload presence row for uid %q in %+v", wantUID, presenceWriter.upserts)
	}
}
