// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

// echoingPostureExistenceReader is the default PostureExistenceReader test
// double for the four posture node writers. With ExistingUIDs left nil it
// answers every candidate uid in the read as existing, so most writer tests
// do not need bespoke read-existence wiring; set ExistingUIDs to restrict
// which uids are reported as existing (used by never-create tests).
type echoingPostureExistenceReader struct {
	ExistingUIDs map[string]bool
	err          error
	calls        []map[string]any
}

func (r *echoingPostureExistenceReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, params)
	if r.err != nil {
		return nil, r.err
	}
	candidates, _ := params["candidate_uids"].([]any)
	rows := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		uid, _ := c.(string)
		if uid == "" {
			continue
		}
		if r.ExistingUIDs != nil && !r.ExistingUIDs[uid] {
			continue
		}
		rows = append(rows, map[string]any{"existing_uid": uid})
	}
	return rows, nil
}

func TestFilterRowsToExistingCloudResourceUIDsRequiresReader(t *testing.T) {
	t.Parallel()

	_, err := filterRowsToExistingCloudResourceUIDs(context.Background(), nil, []map[string]any{{"uid": "a"}})
	if err == nil {
		t.Fatal("filterRowsToExistingCloudResourceUIDs(nil reader) error = nil, want error")
	}
}

func TestFilterRowsToExistingCloudResourceUIDsEmptyRowsIsNoOp(t *testing.T) {
	t.Parallel()

	reader := &echoingPostureExistenceReader{}
	got, err := filterRowsToExistingCloudResourceUIDs(context.Background(), reader, nil)
	if err != nil {
		t.Fatalf("filterRowsToExistingCloudResourceUIDs() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
	if len(reader.calls) != 0 {
		t.Fatalf("reader.calls = %d, want 0 (no read issued for empty rows)", len(reader.calls))
	}
}

func TestFilterRowsToExistingCloudResourceUIDsDropsUnconfirmedUIDs(t *testing.T) {
	t.Parallel()

	reader := &echoingPostureExistenceReader{ExistingUIDs: map[string]bool{"exists-1": true}}
	rows := []map[string]any{
		{"uid": "exists-1", "value": "a"},
		{"uid": "missing-1", "value": "b"},
	}
	got, err := filterRowsToExistingCloudResourceUIDs(context.Background(), reader, rows)
	if err != nil {
		t.Fatalf("filterRowsToExistingCloudResourceUIDs() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0]["uid"] != "exists-1" {
		t.Fatalf("got = %v, want only the confirmed-existing uid row", got)
	}
}

func TestFilterRowsToExistingCloudResourceUIDsDropsRowsWithoutUID(t *testing.T) {
	t.Parallel()

	reader := &echoingPostureExistenceReader{}
	rows := []map[string]any{
		{"uid": "", "value": "a"},
		{"value": "b"},
		{"uid": "real-uid", "value": "c"},
	}
	got, err := filterRowsToExistingCloudResourceUIDs(context.Background(), reader, rows)
	if err != nil {
		t.Fatalf("filterRowsToExistingCloudResourceUIDs() error = %v, want nil", err)
	}
	if len(got) != 1 || got[0]["uid"] != "real-uid" {
		t.Fatalf("got = %v, want only the row with a non-empty uid", got)
	}
}

func TestFilterRowsToExistingCloudResourceUIDsPropagatesReaderError(t *testing.T) {
	t.Parallel()

	reader := &echoingPostureExistenceReader{err: context.DeadlineExceeded}
	_, err := filterRowsToExistingCloudResourceUIDs(context.Background(), reader, []map[string]any{{"uid": "a"}})
	if err == nil {
		t.Fatal("filterRowsToExistingCloudResourceUIDs() error = nil, want propagated reader error")
	}
}
