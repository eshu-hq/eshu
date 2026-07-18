// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"
)

func TestSweepOrphanNodesRequiresExecutorAndReader(t *testing.T) {
	t.Parallel()

	if _, err := (&OrphanSweepStore{}).SweepOrphanNodes(context.Background(), OrphanSweepPolicy{}); err == nil {
		t.Fatal("SweepOrphanNodes() with nil Executor error = nil, want error")
	}

	store := &OrphanSweepStore{Executor: &fakeOrphanGraph{}}
	if _, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{}); err == nil {
		t.Fatal("SweepOrphanNodes() with nil Reader error = nil, want error")
	}

	var nilStore *OrphanSweepStore
	if _, err := nilStore.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{}); err == nil {
		t.Fatal("SweepOrphanNodes() on nil store error = nil, want error")
	}
}

func TestSweepOrphanNodesRejectsUnknownPolicyLabel(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	store := NewOrphanSweepStore(graph, graph)
	_, err := store.SweepOrphanNodes(context.Background(), OrphanSweepPolicy{
		Labels: []string{"NotARealLabel"},
	})
	if err == nil {
		t.Fatal("SweepOrphanNodes() with unknown label error = nil, want error")
	}
}

func TestGraphOrphanNodeCountsRequiresReader(t *testing.T) {
	t.Parallel()

	var nilStore *OrphanSweepStore
	if _, err := nilStore.GraphOrphanNodeCounts(context.Background()); err == nil {
		t.Fatal("GraphOrphanNodeCounts() on nil store error = nil, want error")
	}

	store := &OrphanSweepStore{}
	if _, err := store.GraphOrphanNodeCounts(context.Background()); err == nil {
		t.Fatal("GraphOrphanNodeCounts() with nil Reader error = nil, want error")
	}
}

func TestGraphOrphanNodeCountsUsesDefaultLabelsWhenUnset(t *testing.T) {
	t.Parallel()

	graph := newFakeOrphanGraph()
	graph.seed("Repository", "orphan-1", false, nil)
	store := &OrphanSweepStore{Reader: graph} // Labels left unset

	counts, err := store.GraphOrphanNodeCounts(context.Background())
	if err != nil {
		t.Fatalf("GraphOrphanNodeCounts() error = %v, want nil", err)
	}
	if _, ok := counts["Repository"]; !ok {
		t.Fatalf("counts = %#v, want Repository present via default label set", counts)
	}
}

func TestReadCandidateOrphanNodesRejectsUnexpectedKeyType(t *testing.T) {
	t.Parallel()

	reader := stubOrphanReader{rows: []map[string]any{{"key": 42, "observed_at": nil}}}
	store := &OrphanSweepStore{Reader: reader}
	if _, err := store.readCandidateOrphanNodes(context.Background(), OrphanSweepLabelFile, 10); err == nil {
		t.Fatal("readCandidateOrphanNodes() error = nil, want error for non-string key")
	}
}

func TestReadCandidateOrphanNodesPropagatesReaderError(t *testing.T) {
	t.Parallel()

	reader := stubOrphanReader{err: errStubOrphanReader}
	store := &OrphanSweepStore{Reader: reader}
	if _, err := store.readCandidateOrphanNodes(context.Background(), OrphanSweepLabelFile, 10); err == nil {
		t.Fatal("readCandidateOrphanNodes() error = nil, want propagated reader error")
	}
}

func TestReadConnectedKeysSkipsRoundTripWhenKeysEmpty(t *testing.T) {
	t.Parallel()

	reader := stubOrphanReader{err: errStubOrphanReader}
	store := &OrphanSweepStore{Reader: reader}
	keys, err := store.readConnectedKeys(context.Background(), OrphanSweepLabelFile, nil)
	if err != nil {
		t.Fatalf("readConnectedKeys() error = %v, want nil (no round trip for empty keys)", err)
	}
	if keys != nil {
		t.Fatalf("readConnectedKeys() = %#v, want nil", keys)
	}
}

func TestReadConnectedKeysRejectsUnexpectedKeyType(t *testing.T) {
	t.Parallel()

	reader := stubOrphanReader{rows: []map[string]any{{"key": 7}}}
	store := &OrphanSweepStore{Reader: reader}
	if _, err := store.readConnectedKeys(context.Background(), OrphanSweepLabelFile, []string{"a"}); err == nil {
		t.Fatal("readConnectedKeys() error = nil, want error for non-string key")
	}
}

func TestInt64CountHandlesAllShapes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value any
		want  int64
		ok    bool
	}{
		{"int", 5, 5, true},
		{"int32", int32(6), 6, true},
		{"int64", int64(7), 7, true},
		{"float64 whole", float64(8), 8, true},
		{"float64 fractional", 8.5, 0, false},
		{"float64 negative", -1.0, 0, false},
		{"nil", nil, 0, false},
		{"string", "not-a-number", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := int64Count(tc.value)
			if ok != tc.ok {
				t.Fatalf("int64Count(%#v) ok = %v, want %v", tc.value, ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Fatalf("int64Count(%#v) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestNormalizePositiveIntAndBoundedKeysDefaultOnNonPositive(t *testing.T) {
	t.Parallel()

	if got := normalizePositiveInt(0, 42); got != 42 {
		t.Fatalf("normalizePositiveInt(0, 42) = %d, want 42", got)
	}
	if got := normalizePositiveInt(-5, 42); got != 42 {
		t.Fatalf("normalizePositiveInt(-5, 42) = %d, want 42", got)
	}
	if got := normalizePositiveInt(7, 42); got != 7 {
		t.Fatalf("normalizePositiveInt(7, 42) = %d, want 7", got)
	}

	keys := []string{"a", "b", "c"}
	if got := boundedKeys(keys, 0); len(got) != defaultOrphanSweepBatchLimit && len(got) != len(keys) {
		t.Fatalf("boundedKeys(keys, 0) = %#v, want keys capped at default batch limit", got)
	}
	if got := boundedKeys(keys, 2); len(got) != 2 {
		t.Fatalf("boundedKeys(keys, 2) length = %d, want 2", len(got))
	}
}

func TestOrphanSweepIdentityKeyRejectsUnknownLabel(t *testing.T) {
	t.Parallel()

	if _, ok := orphanSweepIdentityKey(OrphanSweepLabel("Unknown")); ok {
		t.Fatal("orphanSweepIdentityKey() ok = true, want false for unknown label")
	}
}

var errStubOrphanReader = &stubOrphanReaderError{msg: "stub reader error"}

type stubOrphanReaderError struct{ msg string }

func (e *stubOrphanReaderError) Error() string { return e.msg }

type stubOrphanReader struct {
	rows []map[string]any
	err  error
}

func (r stubOrphanReader) Run(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.rows, nil
}
