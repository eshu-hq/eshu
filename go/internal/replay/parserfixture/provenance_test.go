// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parserfixture_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/parserfixture"
)

// recordGoFixture records the Go demo tree to a fresh file and returns the path.
func recordGoFixture(t *testing.T) string {
	t.Helper()
	tc := demoCases()[0] // go
	emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID:  tc.scopeID,
		RepoID:   tc.repoID,
		TreePath: tc.treePath,
	})
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	path := filepath.Join(t.TempDir(), "go.json")
	if err := parserfixture.Record(context.Background(), parserfixture.RecordOptions{
		Emitter: emitter,
		Path:    path,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	return path
}

// TestProvenanceRegressionDroppedSourceURIIsCaught proves the offline gate is
// genuinely failing-capable: if a recorded fixture's provenance (source_uri) is
// dropped — simulating a parser/emitter provenance regression — the replayed
// envelope no longer matches the live one and the round-trip assertion fails.
// This guards against a silent provenance loss going undetected.
func TestProvenanceRegressionDroppedSourceURIIsCaught(t *testing.T) {
	// Live (correct) envelopes straight from the parser + emission seam.
	tc := demoCases()[0]
	emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID:  tc.scopeID,
		RepoID:   tc.repoID,
		TreePath: tc.treePath,
	})
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	live := drainEnvelopes(t, emitter)

	// A clean recording replays identically (control: the gate is green when
	// provenance is intact).
	cleanPath := recordGoFixture(t)
	cleanSrc, err := parserfixture.NewSource(cleanPath)
	if err != nil {
		t.Fatalf("NewSource(clean): %v", err)
	}
	clean := drainEnvelopes(t, cleanSrc)
	if mismatches := countProvenanceMismatches(live, clean); mismatches != 0 {
		t.Fatalf("control: clean recording must match live, got %d provenance mismatches", mismatches)
	}

	// Now mutate the fixture on disk to drop every source_uri, simulating a
	// provenance regression, and replay it.
	brokenPath := mutateFixture(t, cleanPath, func(file map[string]any) {
		scope := file["scope"].(map[string]any)
		for _, raw := range scope["facts"].([]any) {
			fact := raw.(map[string]any)
			delete(fact, "source_uri")
		}
	})
	// The loader requires source_uri, so a fully dropped provenance field is
	// rejected at load — that itself is the gate failing on a provenance
	// regression. Assert the load fails loudly rather than silently replaying.
	if _, err := parserfixture.NewSource(brokenPath); err == nil {
		t.Fatal("expected NewSource to reject a fixture with dropped source_uri provenance, got nil error")
	}
}

// TestProvenanceRegressionChangedSourceURIIsCaught proves a CHANGED (not dropped)
// provenance value is caught by the round-trip equality assertion: the loader
// accepts the fixture (source_uri is still present), but the replayed envelope's
// provenance no longer matches the live one.
func TestProvenanceRegressionChangedSourceURIIsCaught(t *testing.T) {
	tc := demoCases()[0]
	emitter, err := parserfixture.NewEmitter(parserfixture.EmitterOptions{
		ScopeID:  tc.scopeID,
		RepoID:   tc.repoID,
		TreePath: tc.treePath,
	})
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	live := drainEnvelopes(t, emitter)

	cleanPath := recordGoFixture(t)
	brokenPath := mutateFixture(t, cleanPath, func(file map[string]any) {
		scope := file["scope"].(map[string]any)
		facts := scope["facts"].([]any)
		// Corrupt the provenance of the first fact only.
		facts[0].(map[string]any)["source_uri"] = "/tampered/provenance/path"
	})
	brokenSrc, err := parserfixture.NewSource(brokenPath)
	if err != nil {
		t.Fatalf("NewSource(broken): %v", err)
	}
	broken := drainEnvelopes(t, brokenSrc)

	if mismatches := countProvenanceMismatches(live, broken); mismatches == 0 {
		t.Fatal("expected a changed source_uri to be caught as a provenance mismatch, got 0")
	}
}

// TestRecordIsByteIdenticalOnReRecord proves canonical determinism: recording the
// same tree twice yields byte-identical fixtures, so a fixture diff reflects a
// real change in parser output or provenance, not record-order churn.
func TestRecordIsByteIdenticalOnReRecord(t *testing.T) {
	first := recordGoFixture(t)
	second := recordGoFixture(t)
	a, err := os.ReadFile(first) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	b, err := os.ReadFile(second) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("re-record produced non-identical bytes; canonicalization is not deterministic")
	}
}

// countProvenanceMismatches counts envelopes (matched by stable fact key) whose
// SourceRef provenance differs between two envelope sets. It is the single
// provenance-equality predicate the positive and negative tests share, so the
// negative test exercises the same comparison the acceptance test relies on.
func countProvenanceMismatches(want, got []facts.Envelope) int {
	mismatches := 0
	byKey := make(map[string]facts.Ref, len(got))
	for _, e := range got {
		byKey[e.StableFactKey] = e.SourceRef
	}
	for _, w := range want {
		g, ok := byKey[w.StableFactKey]
		if !ok {
			mismatches++
			continue
		}
		if w.SourceRef.SourceURI != g.SourceURI ||
			w.SourceRef.SourceRecordID != g.SourceRecordID ||
			w.SourceRef.SourceSystem != g.SourceSystem {
			mismatches++
		}
	}
	return mismatches
}

// mutateFixture loads a fixture file as raw JSON, applies mutate, writes it back
// to a sibling path, and returns that path. It lets a test simulate a corrupt
// recording without going through the validating loader on write.
func mutateFixture(t *testing.T, path string, mutate func(map[string]any)) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temp path
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var file map[string]any
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	mutate(file)
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal mutated fixture: %v", err)
	}
	brokenPath := filepath.Join(filepath.Dir(path), "broken.json")
	if err := os.WriteFile(brokenPath, out, 0o644); err != nil { // #nosec G306 -- test fixture
		t.Fatalf("write mutated fixture: %v", err)
	}
	return brokenPath
}
