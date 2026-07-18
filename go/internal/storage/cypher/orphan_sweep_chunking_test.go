// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"fmt"
	"testing"
)

// chunkCountingOrphanReader records every keys parameter it was called with,
// so tests can assert both the number of round trips readConnectedKeys
// issues and that the union of per-round-trip results is complete and
// deduplicated. It treats every supplied key as connected, so the returned
// rows are exactly the keys passed in that call.
type chunkCountingOrphanReader struct {
	calls [][]string
}

func (r *chunkCountingOrphanReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	keys, _ := params["keys"].([]string)
	r.calls = append(r.calls, append([]string{}, keys...))
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

func TestReadConnectedKeysIssuesOneRoundTripAtOrBelowChunkSize(t *testing.T) {
	t.Parallel()

	reader := &chunkCountingOrphanReader{}
	store := &OrphanSweepStore{Reader: reader}

	keys := make([]string, defaultOrphanSweepConnectedKeysChunkSize)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	got, err := store.readConnectedKeys(context.Background(), OrphanSweepLabelFile, keys)
	if err != nil {
		t.Fatalf("readConnectedKeys() error = %v, want nil", err)
	}
	if len(reader.calls) != 1 {
		t.Fatalf("round trips = %d, want 1 for exactly chunk-size keys", len(reader.calls))
	}
	if len(got) != len(keys) {
		t.Fatalf("connected keys = %d, want %d", len(got), len(keys))
	}
}

func TestReadConnectedKeysChunksAboveChunkSizeAndUnionsResults(t *testing.T) {
	t.Parallel()

	reader := &chunkCountingOrphanReader{}
	store := &OrphanSweepStore{Reader: reader}

	// #5147 finding 2: the UNWIND-per-key anchored S2 read's own
	// per-statement cost scales super-linearly with key-list size on both
	// pinned NornicDB backends (measured ~4.7s at 5,000 keys unchunked vs
	// ~570ms chunked at 500 keys/round trip -- see
	// evidence-5147-orphan-sweep-antijoin.md). readConnectedKeys must split
	// any key list larger than defaultOrphanSweepConnectedKeysChunkSize into
	// multiple bounded round trips rather than issue one unbounded anchor.
	total := defaultOrphanSweepConnectedKeysChunkSize*2 + 7
	keys := make([]string, total)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}

	got, err := store.readConnectedKeys(context.Background(), OrphanSweepLabelFile, keys)
	if err != nil {
		t.Fatalf("readConnectedKeys() error = %v, want nil", err)
	}

	wantCalls := 3 // ceil(total / chunkSize) = ceil(1007/500) = 3
	if len(reader.calls) != wantCalls {
		t.Fatalf("round trips = %d, want %d", len(reader.calls), wantCalls)
	}
	for i, call := range reader.calls {
		if len(call) > defaultOrphanSweepConnectedKeysChunkSize {
			t.Fatalf("round trip %d requested %d keys, want <= %d", i, len(call), defaultOrphanSweepConnectedKeysChunkSize)
		}
	}
	if len(got) != total {
		t.Fatalf("union of connected keys = %d, want %d (no keys dropped or duplicated)", len(got), total)
	}
	seen := make(map[string]bool, len(got))
	for _, k := range got {
		if seen[k] {
			t.Fatalf("connected keys contain duplicate %q", k)
		}
		seen[k] = true
	}
	for _, k := range keys {
		if !seen[k] {
			t.Fatalf("connected keys missing input key %q", k)
		}
	}
}

func TestReadConnectedKeysChunkPropagatesReaderErrorMidway(t *testing.T) {
	t.Parallel()

	reader := failAfterNCallsOrphanReader{failAfter: 1}
	store := &OrphanSweepStore{Reader: &reader}

	keys := make([]string, defaultOrphanSweepConnectedKeysChunkSize*2+1)
	for i := range keys {
		keys[i] = fmt.Sprintf("key-%d", i)
	}
	if _, err := store.readConnectedKeys(context.Background(), OrphanSweepLabelFile, keys); err == nil {
		t.Fatal("readConnectedKeys() error = nil, want propagated reader error from a later chunk")
	}
}

// failAfterNCallsOrphanReader succeeds for the first failAfter calls, then
// errors -- used to prove a mid-chunk failure surfaces rather than being
// silently swallowed.
type failAfterNCallsOrphanReader struct {
	calls     int
	failAfter int
}

func (r *failAfterNCallsOrphanReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	r.calls++
	if r.calls > r.failAfter {
		return nil, errStubOrphanReader
	}
	keys, _ := params["keys"].([]string)
	rows := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}
