// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recorder_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/recorder"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

var updateGolden = flag.Bool("update", false, "rewrite the recorded cassette golden file")

// fakeSource yields a fixed list of generations once, then reports the batch
// exhausted — the same one-batch-per-poll contract the live and cassette
// sources follow.
type fakeSource struct {
	gens    []collector.CollectedGeneration
	index   int
	drained bool
}

func (f *fakeSource) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	if f.drained || f.index >= len(f.gens) {
		f.drained = true
		return collector.CollectedGeneration{}, false, nil
	}
	gen := f.gens[f.index]
	f.index++
	return gen, true, nil
}

// objectID mirrors how the real kuberneteslive collector derives a fact's
// opaque object_id, so the round-trip test proves the recorder preserves that
// exact value (the structural #3928 fix) rather than a hand-authored stand-in.
func objectID(kind, namespace, name string) string {
	return facts.StableID("KubernetesLiveObject", map[string]any{
		"api_version": "apps/v1",
		"kind":        kind,
		"namespace":   namespace,
		"name":        name,
	})
}

func sampleEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind:         "kubernetes_workload",
			StableFactKey:    "default/deploy/app",
			SchemaVersion:    "1",
			CollectorKind:    "kubernetes_live",
			FencingToken:     1,
			SourceConfidence: "observed",
			Payload: map[string]any{
				"object_id": objectID("Deployment", "default", "app"),
				"name":      "app",
				"replicas":  float64(3),
			},
			// A source-record id distinct from the stable key must round-trip
			// verbatim (most collectors set these differently).
			SourceRef: facts.Ref{SourceURI: "k8s://default/deploy/app", SourceRecordID: "k8s-uid-app-001"},
		},
		{
			FactKind:         "kubernetes_workload",
			StableFactKey:    "default/deploy/api",
			SchemaVersion:    "1",
			CollectorKind:    "kubernetes_live",
			FencingToken:     1,
			SourceConfidence: "observed",
			Payload: map[string]any{
				"object_id": objectID("Deployment", "default", "api"),
				"name":      "api",
				"replicas":  float64(2),
			},
			SourceRef: facts.Ref{SourceURI: "k8s://default/deploy/api"},
		},
	}
}

func genFrom(scopeID, genID string, observedAt time.Time, envs []facts.Envelope) collector.CollectedGeneration {
	s := scope.IngestionScope{
		ScopeID:       scopeID,
		SourceSystem:  "kubernetes_live",
		ScopeKind:     scope.KindCluster,
		CollectorKind: scope.CollectorKind("kubernetes_live"),
	}
	g := scope.ScopeGeneration{
		GenerationID: genID,
		ScopeID:      scopeID,
		ObservedAt:   observedAt,
		IngestedAt:   observedAt,
		Status:       scope.GenerationStatusPending,
		TriggerKind:  scope.TriggerKind("snapshot"),
	}
	return collector.FactsFromSlice(s, g, envs)
}

func recordToFile(t *testing.T, gens ...collector.CollectedGeneration) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recorded.json")
	src := &fakeSource{gens: gens}
	if err := recorder.Run(context.Background(), src, recorder.Options{Path: path, CollectorLabel: "kubernetes_live"}); err != nil {
		t.Fatalf("recorder.Run() error = %v", err)
	}
	return path
}

func replayAll(t *testing.T, path string) []facts.Envelope {
	t.Helper()
	src, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource(%q) error = %v", path, err)
	}
	var out []facts.Envelope
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("cassette Next error = %v", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StableFactKey < out[j].StableFactKey })
	return out
}

// TestRecordReplayRoundTripPreservesFacts is the core fidelity proof: every fact
// field the recorder writes — most importantly the opaque object_id the
// collector derived — survives record → canonical cassette → replay unchanged.
func TestRecordReplayRoundTripPreservesFacts(t *testing.T) {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	originals := sampleEnvelopes()
	path := recordToFile(t, genFrom("cluster-a", "gen-real-123", observedAt, originals))

	replayed := replayAll(t, path)
	if len(replayed) != len(originals) {
		t.Fatalf("replayed %d facts, want %d", len(replayed), len(originals))
	}

	wantByKey := map[string]facts.Envelope{}
	for _, e := range originals {
		wantByKey[e.StableFactKey] = e
	}
	for _, got := range replayed {
		want := wantByKey[got.StableFactKey]
		if got.FactKind != want.FactKind {
			t.Errorf("%s FactKind = %q, want %q", got.StableFactKey, got.FactKind, want.FactKind)
		}
		if got.SchemaVersion != want.SchemaVersion {
			t.Errorf("%s SchemaVersion = %q, want %q", got.StableFactKey, got.SchemaVersion, want.SchemaVersion)
		}
		if got.CollectorKind != want.CollectorKind {
			t.Errorf("%s CollectorKind = %q, want %q", got.StableFactKey, got.CollectorKind, want.CollectorKind)
		}
		if got.SourceConfidence != want.SourceConfidence {
			t.Errorf("%s SourceConfidence = %q, want %q", got.StableFactKey, got.SourceConfidence, want.SourceConfidence)
		}
		if gotOID, wantOID := got.Payload["object_id"], want.Payload["object_id"]; gotOID != wantOID {
			t.Errorf("%s object_id = %v, want %v (object_id fidelity / #3928)", got.StableFactKey, gotOID, wantOID)
		}
		if got.SourceRef.SourceURI != want.SourceRef.SourceURI {
			t.Errorf("%s SourceURI = %q, want %q", got.StableFactKey, got.SourceRef.SourceURI, want.SourceRef.SourceURI)
		}
		// SourceRecordID round-trips verbatim when distinct; when the collector
		// left it equal to the stable key the cassette omits it and replay
		// defaults it back to the key.
		wantRecordID := want.SourceRef.SourceRecordID
		if wantRecordID == "" {
			wantRecordID = want.StableFactKey
		}
		if got.SourceRef.SourceRecordID != wantRecordID {
			t.Errorf("%s SourceRecordID = %q, want %q (provenance fidelity)", got.StableFactKey, got.SourceRef.SourceRecordID, wantRecordID)
		}
	}
}

// TestRecordIsCanonicalAndStable proves the recorder is deterministic across a
// record → replay → record cycle: the bytes are byte-identical, which is what
// makes a re-recorded fixture produce a reviewable (empty) diff.
func TestRecordIsCanonicalAndStable(t *testing.T) {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)

	firstPath := recordToFile(t, genFrom("cluster-a", "gen-real-123", observedAt, sampleEnvelopes()))
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatalf("read first recording: %v", err)
	}

	// Re-record from the replay of the first cassette. A different raw
	// generation_id and observed_at on the way in must still canonicalize to the
	// same bytes.
	replayed := replayAll(t, firstPath)
	secondPath := recordToFile(t, genFrom("cluster-a", "gen-different-999", observedAt.Add(time.Hour), replayed))
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatalf("read second recording: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("record→replay→record is not byte-identical:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestRecordedCassetteMatchesGolden commits a concrete recorded cassette so a
// reviewer can read the canonical output, and gates it against drift. Regenerate
// with: go test ./internal/replay/recorder -run TestRecordedCassetteMatchesGolden -update
func TestRecordedCassetteMatchesGolden(t *testing.T) {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := recordToFile(t, genFrom("cluster-a", "gen-real-123", observedAt, sampleEnvelopes()))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}

	golden := filepath.Join("testdata", "pilot.recorded.json")
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0o644); err != nil { //nolint:gosec // committed cassette fixture
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %q: %v (re-run with -update)", golden, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("recorded cassette differs from golden; re-run with -update\ngot:\n%s", got)
	}
}

func TestRunRejectsEmptyBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	err := recorder.Run(context.Background(), &fakeSource{}, recorder.Options{Path: path, CollectorLabel: "kubernetes_live"})
	if err == nil {
		t.Fatal("recorder.Run() on empty source = nil, want error")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("recorder wrote a file for an empty batch; want no file")
	}
}

func TestRunRequiresPath(t *testing.T) {
	observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	src := &fakeSource{gens: []collector.CollectedGeneration{genFrom("c", "g", observedAt, sampleEnvelopes())}}
	if err := recorder.Run(context.Background(), src, recorder.Options{}); err == nil {
		t.Fatal("recorder.Run() without path = nil, want error")
	}
}
