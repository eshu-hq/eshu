// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// TestProcessPartitionOnceHandlesRouteDrainsAbsentEndpoint proves the terminal
// semantics through the full partition cycle (#2809 liveness): a handles_route
// intent whose (repo_id, path) Endpoint is absent is marked complete (drained,
// NOT re-enqueued) with no edge write, while a sibling with a present endpoint
// projects an edge. The absent row is still retracted (repo-scoped) so a stale
// edge cannot survive, and it is reported as terminal-no-endpoint, never blocked.
func TestProcessPartitionOnceHandlesRouteDrainsAbsentEndpoint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("present", "repo-1", "/present"),
			handlesRouteIntentRow("absent", "repo-1", "/absent"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{
		apiEndpointRepoPathPresenceKey("repo-1", "/present"): {},
	}}

	cfg := PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, presence, nil, nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (absent endpoint is terminal, not deferred)", result.BlockedReadiness)
	}
	if result.TerminalNoEndpoint != 1 {
		t.Fatalf("TerminalNoEndpoint = %d, want 1 (the absent-endpoint row)", result.TerminalNoEndpoint)
	}
	// Both intents are marked complete — the present one as a projected edge, the
	// absent one drained with no edge. The backlog must not retain the absent row.
	completed := map[string]bool{}
	for _, id := range reader.completedIDs {
		completed[id] = true
	}
	if !completed[reader.pending[0].IntentID] || !completed[reader.pending[1].IntentID] {
		t.Fatalf("completedIDs = %v, want both present and absent intents drained", reader.completedIDs)
	}
	// The edge write set contains only the present-endpoint row; the absent row
	// produces no HANDLES_ROUTE edge.
	var writtenIDs []string
	for _, batch := range edges.writeCalls {
		for _, row := range batch {
			writtenIDs = append(writtenIDs, row.IntentID)
		}
	}
	if len(writtenIDs) != 1 || writtenIDs[0] != "present" {
		t.Fatalf("written rows = %v, want only the present-endpoint intent", writtenIDs)
	}
	// The absent row is retracted (repo-scoped cleanup), so no stale edge survives.
	var retractedIDs []string
	for _, batch := range edges.retractCalls {
		for _, row := range batch {
			retractedIDs = append(retractedIDs, row.IntentID)
		}
	}
	if !containsIntentID(retractedIDs, "absent") {
		t.Fatalf("retracted rows = %v, want the absent-endpoint row included for stale-edge cleanup", retractedIDs)
	}
}

func containsIntentID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestProcessPartitionOnceHandlesRouteAllTerminalRepoDrainsAndRetracts covers the
// route-only repo: every (repo_id, path) Endpoint is absent, so there are zero
// ready rows. The cycle must still run (not early-return as idle), drain every
// terminal row, and retract repo-scoped so a stale edge from a prior generation
// (when the endpoint existed) is cleared. This is the liveness case that would
// otherwise stall the backlog forever.
func TestProcessPartitionOnceHandlesRouteAllTerminalRepoDrainsAndRetracts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("route-a", "repo-1", "/a"),
			handlesRouteIntentRow("route-b", "repo-1", "/b"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}
	presence := &fakeRepoPathPresenceLookup{present: map[string]struct{}{}} // nothing present

	cfg := PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, presence, nil, nil,
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (route-only repo is terminal, not deferred)", result.BlockedReadiness)
	}
	if result.TerminalNoEndpoint != 2 {
		t.Fatalf("TerminalNoEndpoint = %d, want 2 (both routes have no endpoint)", result.TerminalNoEndpoint)
	}
	if result.ProcessedIntents != 2 {
		t.Fatalf("ProcessedIntents = %d, want 2 (both drained, none left pending)", result.ProcessedIntents)
	}
	if len(reader.completedIDs) != 2 {
		t.Fatalf("completedIDs = %v, want both routes drained", reader.completedIDs)
	}
	// No edge written, but the repo is retracted so a stale edge cannot survive.
	for _, batch := range edges.writeCalls {
		if len(batch) != 0 {
			t.Fatalf("write batch = %v, want no HANDLES_ROUTE edge written for an all-terminal repo", batch)
		}
	}
	var retractedIDs []string
	for _, batch := range edges.retractCalls {
		for _, row := range batch {
			retractedIDs = append(retractedIDs, row.IntentID)
		}
	}
	if !containsIntentID(retractedIDs, "route-a") || !containsIntentID(retractedIDs, "route-b") {
		t.Fatalf("retracted rows = %v, want both terminal rows for repo-scoped stale-edge cleanup", retractedIDs)
	}
}

// TestProcessPartitionOnceHandlesRouteNilPresenceProjectsAll proves the
// nil-presence fallback: with no presence lookup wired, handles_route behaves
// exactly as today — the phase gate alone decides, and all phase-ready rows
// project.
func TestProcessPartitionOnceHandlesRouteNilPresenceProjectsAll(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 17, 14, 0, 0, 0, time.UTC)
	reader := &stubSharedIntentReader{
		pending: []SharedProjectionIntentRow{
			handlesRouteIntentRow("a", "repo-1", "/a"),
			handlesRouteIntentRow("b", "repo-1", "/b"),
		},
	}
	lease := &stubLeaseManager{claimResult: true}
	edges := &stubEdgeWriter{}

	cfg := PartitionProcessorConfig{
		Domain:         DomainHandlesRoute,
		PartitionID:    0,
		PartitionCount: 1,
		LeaseOwner:     "worker-1",
		LeaseTTL:       30 * time.Second,
		BatchLimit:     100,
		EvidenceSource: handlesRouteEvidenceSource,
	}

	result, err := ProcessPartitionOnce(
		context.Background(), now, cfg, lease, reader, edges,
		acceptedGenerationFixed("gen-1", true), nil,
		readinessLookupFixed(true, true), nil, nil, nil, nil, // nil presence lookup
	)
	if err != nil {
		t.Fatalf("ProcessPartitionOnce() error = %v", err)
	}
	if result.BlockedReadiness != 0 {
		t.Fatalf("BlockedReadiness = %d, want 0 (nil presence = today's behavior)", result.BlockedReadiness)
	}
	if result.UpsertedRows != 2 {
		t.Fatalf("UpsertedRows = %d, want 2 (both project with nil presence)", result.UpsertedRows)
	}
}
