// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"
)

// recordingPresenceWriter captures Upsert/RetractScope/RetractStaleRepoGenerations
// calls for assertions.
type recordingPresenceWriter struct {
	upserts      [][]EndpointPresenceRow
	retracts     [][]string
	staleRetract []staleRepoGenerationRetract
	err          error
}

// staleRepoGenerationRetract records one RetractStaleRepoGenerations call (#2842).
type staleRepoGenerationRetract struct {
	keyspace     GraphProjectionKeyspace
	scopeID      string
	generationID string
	repoIDs      []string
}

func (w *recordingPresenceWriter) Upsert(_ context.Context, rows []EndpointPresenceRow) error {
	if w.err != nil {
		return w.err
	}
	w.upserts = append(w.upserts, rows)
	return nil
}

func (w *recordingPresenceWriter) RetractScope(_ context.Context, scopeIDs []string) error {
	w.retracts = append(w.retracts, scopeIDs)
	return nil
}

func (w *recordingPresenceWriter) RetractStaleRepoGenerations(
	_ context.Context,
	keyspace GraphProjectionKeyspace,
	scopeID, generationID string,
	repoIDs []string,
) error {
	if w.err != nil {
		return w.err
	}
	w.staleRetract = append(w.staleRetract, staleRepoGenerationRetract{
		keyspace:     keyspace,
		scopeID:      scopeID,
		generationID: generationID,
		repoIDs:      repoIDs,
	})
	return nil
}

func TestPublishEndpointPresenceNilWriterIsNoOp(t *testing.T) {
	t.Parallel()

	// The flag-off path: a nil writer must never touch presence, so the hot
	// materializer paths carry zero extra work when the feature is disabled.
	if err := publishEndpointPresence(context.Background(), nil,
		GraphProjectionKeyspaceCloudResourceUID, "scope-1",
		[]map[string]any{{"uid": "cr-1"}}, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("nil writer error = %v, want nil", err)
	}
}

func TestPublishEndpointPresenceUpsertsNodeUIDs(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	rows := []map[string]any{{"uid": "cr-1"}, {"uid": ""}, {"uid": "cr-2"}, {"other": "x"}}
	if err := publishEndpointPresence(context.Background(), writer,
		GraphProjectionKeyspaceCloudResourceUID, "scope-1", rows, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("Upsert error = %v", err)
	}
	if len(writer.upserts) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(writer.upserts))
	}
	got := writer.upserts[0]
	if len(got) != 2 {
		t.Fatalf("presence rows = %d, want 2 (blank/uid-less rows skipped)", len(got))
	}
	for _, r := range got {
		if r.Keyspace != GraphProjectionKeyspaceCloudResourceUID || r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("malformed presence row: %+v", r)
		}
	}
}

func TestPublishEndpointPresenceEmptyRowsNoCall(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	if err := publishEndpointPresence(context.Background(), writer,
		GraphProjectionKeyspaceKubernetesWorkloadUID, "scope-1", nil, time.Now().UTC()); err != nil {
		t.Fatalf("empty rows error = %v", err)
	}
	if len(writer.upserts) != 0 {
		t.Fatalf("upsert calls = %d, want 0 for empty node rows", len(writer.upserts))
	}
}
