// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

import (
	"context"
	"testing"
	"time"
)

// recordingPresenceWriter captures Upsert calls for assertions, mirroring the
// reducer root's own test double for this interface before the move.
type recordingPresenceWriter struct {
	upserts [][]EndpointPresenceRow
	err     error
}

func (w *recordingPresenceWriter) Upsert(_ context.Context, rows []EndpointPresenceRow) error {
	if w.err != nil {
		return w.err
	}
	w.upserts = append(w.upserts, rows)
	return nil
}

func (w *recordingPresenceWriter) RetractScope(_ context.Context, _ []string) error {
	return nil
}

func (w *recordingPresenceWriter) RetractStaleRepoGenerations(
	_ context.Context, _ Keyspace, _, _ string, _ []string,
) error {
	return nil
}

func TestPublishEndpointPresenceNilWriterIsNoOp(t *testing.T) {
	t.Parallel()

	if err := PublishEndpointPresence(context.Background(), nil,
		KeyspaceCloudResourceUID, "scope-1",
		[]map[string]any{{"uid": "cr-1"}}, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("PublishEndpointPresence() error = %v, want nil", err)
	}
}

func TestPublishEndpointPresenceUpsertsNodeUIDs(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	rows := []map[string]any{{"uid": "cr-1"}, {"uid": ""}, {"uid": "cr-2"}, {"other": "x"}}
	if err := PublishEndpointPresence(context.Background(), writer,
		KeyspaceCloudResourceUID, "scope-1", rows, time.Unix(1700000000, 0).UTC()); err != nil {
		t.Fatalf("PublishEndpointPresence() error = %v", err)
	}
	if len(writer.upserts) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(writer.upserts))
	}
	got := writer.upserts[0]
	if len(got) != 2 {
		t.Fatalf("presence rows = %d, want 2 (blank/uid-less rows skipped)", len(got))
	}
	for _, r := range got {
		if r.Keyspace != KeyspaceCloudResourceUID || r.ScopeID != "scope-1" || r.UID == "" {
			t.Fatalf("malformed presence row: %+v", r)
		}
	}
}

func TestPublishEndpointPresenceEmptyRowsNoCall(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{}
	if err := PublishEndpointPresence(context.Background(), writer,
		KeyspaceKubernetesWorkloadUID, "scope-1", nil, time.Now().UTC()); err != nil {
		t.Fatalf("PublishEndpointPresence() error = %v", err)
	}
	if len(writer.upserts) != 0 {
		t.Fatalf("upsert calls = %d, want 0 for empty node rows", len(writer.upserts))
	}
}

func TestPublishEndpointPresenceWriterErrorPropagates(t *testing.T) {
	t.Parallel()

	writer := &recordingPresenceWriter{err: context.Canceled}
	err := PublishEndpointPresence(context.Background(), writer,
		KeyspaceCloudResourceUID, "scope-1", []map[string]any{{"uid": "cr-1"}}, time.Now().UTC())
	if err == nil {
		t.Fatal("PublishEndpointPresence() error = nil, want non-nil")
	}
}
