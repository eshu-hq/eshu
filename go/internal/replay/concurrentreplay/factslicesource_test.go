// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fakeFactLoader is a minimal concurrentreplay.FactLoader that returns a
// fixed slice of envelopes per projector.ScopeGenerationWork descriptor,
// keyed by (ScopeID, GenerationID), and counts how many times LoadFacts was
// invoked per key so tests can assert one-shot delivery semantics.
type fakeFactLoader struct {
	mu    sync.Mutex
	envs  map[string][]facts.Envelope
	calls map[string]int
}

func newFakeFactLoader(envs map[string][]facts.Envelope) *fakeFactLoader {
	return &fakeFactLoader{envs: envs, calls: make(map[string]int)}
}

func loaderKey(work projector.ScopeGenerationWork) string {
	return work.Scope.ScopeID + "|" + work.Generation.GenerationID
}

// LoadFacts returns the fixed envelopes recorded for work's (Scope,
// Generation) key, and records one call against that key.
func (f *fakeFactLoader) LoadFacts(
	_ context.Context,
	work projector.ScopeGenerationWork,
) ([]facts.Envelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := loaderKey(work)
	f.calls[key]++
	return f.envs[key], nil
}

// callCount returns how many times LoadFacts was invoked for the given
// descriptor's key.
func (f *fakeFactLoader) callCount(work projector.ScopeGenerationWork) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[loaderKey(work)]
}

// erroringFactLoader always returns wantErr, and counts invocations so a
// test can prove wrapping preserves the error without hiding it.
type erroringFactLoader struct {
	mu      sync.Mutex
	calls   int
	wantErr error
}

func (f *erroringFactLoader) LoadFacts(
	_ context.Context,
	_ projector.ScopeGenerationWork,
) ([]facts.Envelope, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil, f.wantErr
}

// scopeGenerationWork builds a minimal descriptor for the given scope and
// generation IDs, matching the shape a reducer claim or a recorded drain
// manifest would carry.
func scopeGenerationWork(scopeID, generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:      scope.IngestionScope{ScopeID: scopeID, SourceSystem: "git"},
		Generation: scope.ScopeGeneration{GenerationID: generationID},
	}
}

// factEnvelope builds one minimal fact envelope for scopeID/generationID
// with a distinguishing StableFactKey so ordering can be asserted.
func factEnvelope(scopeID, generationID, stableFactKey string) facts.Envelope {
	return facts.Envelope{
		ScopeID:       scopeID,
		GenerationID:  generationID,
		FactKind:      "git.commit",
		StableFactKey: stableFactKey,
		SchemaVersion: "1.0.0",
		CollectorKind: "git",
		ObservedAt:    time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		Payload:       map[string]any{"sha": stableFactKey},
	}
}

// drainFacts drains gen.Facts to a slice, in delivery order, so a test can
// compare it against the loader's recorded output.
func drainFacts(t *testing.T, ch <-chan facts.Envelope) []facts.Envelope {
	t.Helper()
	var out []facts.Envelope
	for e := range ch {
		out = append(out, e)
	}
	return out
}

// TestFactSliceSourceReplaysRecordedSlices proves that FactSliceSource, driven
// single-threaded, hands out one CollectedGeneration per configured
// projector.ScopeGenerationWork descriptor, in order, with Scope/Generation
// matching the descriptor and the drained Facts channel matching the fake
// loader's output for that descriptor exactly (including order) — then
// reports permanent exhaustion (ok=false, err=nil) after the last one, with
// no restart on further calls.
func TestFactSliceSourceReplaysRecordedSlices(t *testing.T) {
	t.Parallel()

	workA := scopeGenerationWork("git:repo:alpha", "gen-alpha-1")
	workB := scopeGenerationWork("git:repo:beta", "gen-beta-1")

	envsA := []facts.Envelope{
		factEnvelope("git:repo:alpha", "gen-alpha-1", "alpha:sha:1"),
		factEnvelope("git:repo:alpha", "gen-alpha-1", "alpha:sha:2"),
	}
	envsB := []facts.Envelope{
		factEnvelope("git:repo:beta", "gen-beta-1", "beta:sha:1"),
	}

	loader := newFakeFactLoader(map[string][]facts.Envelope{
		loaderKey(workA): envsA,
		loaderKey(workB): envsB,
	})

	src := concurrentreplay.NewFactSliceSource(loader, []projector.ScopeGenerationWork{workA, workB})

	ctx := context.Background()

	gen, ok, err := src.Next(ctx)
	if err != nil {
		t.Fatalf("Next[0]: unexpected error %v", err)
	}
	if !ok {
		t.Fatal("Next[0]: ok=false, want true")
	}
	if got, want := gen.Scope.ScopeID, workA.Scope.ScopeID; got != want {
		t.Errorf("Next[0]: Scope.ScopeID = %q, want %q", got, want)
	}
	if got, want := gen.Generation.GenerationID, workA.Generation.GenerationID; got != want {
		t.Errorf("Next[0]: Generation.GenerationID = %q, want %q", got, want)
	}
	drainedA := drainFacts(t, gen.Facts)
	if got, want := len(drainedA), len(envsA); got != want {
		t.Fatalf("Next[0]: drained %d facts, want %d", got, want)
	}
	for i, e := range drainedA {
		if e.StableFactKey != envsA[i].StableFactKey {
			t.Errorf("Next[0]: fact[%d].StableFactKey = %q, want %q", i, e.StableFactKey, envsA[i].StableFactKey)
		}
	}

	gen, ok, err = src.Next(ctx)
	if err != nil {
		t.Fatalf("Next[1]: unexpected error %v", err)
	}
	if !ok {
		t.Fatal("Next[1]: ok=false, want true")
	}
	if got, want := gen.Scope.ScopeID, workB.Scope.ScopeID; got != want {
		t.Errorf("Next[1]: Scope.ScopeID = %q, want %q", got, want)
	}
	if got, want := gen.Generation.GenerationID, workB.Generation.GenerationID; got != want {
		t.Errorf("Next[1]: Generation.GenerationID = %q, want %q", got, want)
	}
	drainedB := drainFacts(t, gen.Facts)
	if got, want := len(drainedB), len(envsB); got != want {
		t.Fatalf("Next[1]: drained %d facts, want %d", got, want)
	}
	if drainedB[0].StableFactKey != envsB[0].StableFactKey {
		t.Errorf("Next[1]: fact[0].StableFactKey = %q, want %q", drainedB[0].StableFactKey, envsB[0].StableFactKey)
	}

	// One-shot: after the last descriptor, Next must report permanent
	// exhaustion, never restarting the slice.
	for i := 0; i < 3; i++ {
		gen, ok, err := src.Next(ctx)
		if err != nil {
			t.Fatalf("post-drain Next[%d]: unexpected error %v", i, err)
		}
		if ok {
			t.Fatalf("post-drain Next[%d]: ok=true (generation %q), want false forever", i, gen.Generation.GenerationID)
		}
	}

	if got, want := loader.callCount(workA), 1; got != want {
		t.Errorf("loader called %d times for workA, want %d (one-shot)", got, want)
	}
	if got, want := loader.callCount(workB), 1; got != want {
		t.Errorf("loader called %d times for workB, want %d (one-shot)", got, want)
	}
}

// TestFactSliceSourceWrapsLoaderError proves that a FactLoader error is
// wrapped (so errors.Is still matches it) and surfaced on the call that
// observed it.
func TestFactSliceSourceWrapsLoaderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom: fake fact store failure")
	loader := &erroringFactLoader{wantErr: wantErr}
	work := scopeGenerationWork("git:repo:gamma", "gen-gamma-1")

	src := concurrentreplay.NewFactSliceSource(loader, []projector.ScopeGenerationWork{work})

	_, ok, err := src.Next(context.Background())
	if ok {
		t.Fatal("Next: ok=true, want false on loader error")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Next: err=%v, want it to wrap %v", err, wantErr)
	}
}

// TestFactSliceSourceUnderDriver wraps a FactSliceSource in
// concurrentreplay.NewSource and drives it with a concurrent Driver, proving
// exact-once delivery of every descriptor's generation under -race — the
// same concurrency proof driver_test.go runs for scriptedGenerationSource,
// but here the delegate is the real git LoadFacts replay path instead of a
// scripted fake.
func TestFactSliceSourceUnderDriver(t *testing.T) {
	t.Parallel()

	const descriptorCount = 40
	works := make([]projector.ScopeGenerationWork, 0, descriptorCount)
	envs := make(map[string][]facts.Envelope, descriptorCount)
	for i := 0; i < descriptorCount; i++ {
		scopeID := fmt.Sprintf("git:repo:%03d", i)
		generationID := fmt.Sprintf("gen-%03d", i)
		work := scopeGenerationWork(scopeID, generationID)
		works = append(works, work)
		envs[loaderKey(work)] = []facts.Envelope{factEnvelope(scopeID, generationID, scopeID+":sha:1")}
	}

	loader := newFakeFactLoader(envs)
	delegate := concurrentreplay.NewFactSliceSource(loader, works)
	src := concurrentreplay.NewSource(delegate)
	committer := &recordingCommitter{}

	driver := concurrentreplay.Driver{
		Source:    src,
		Committer: committer,
		Workers:   8,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	report, err := driver.Run(ctx)
	if err != nil {
		t.Fatalf("Run: unexpected error %v", err)
	}
	if got, want := report.GenerationsCommitted, descriptorCount; got != want {
		t.Fatalf("Report.GenerationsCommitted = %d, want %d", got, want)
	}

	committed := committer.snapshot()
	if got, want := len(committed), descriptorCount; got != want {
		t.Fatalf("committed %d generations, want exactly %d (no dup/missing)", got, want)
	}
	seen := make(map[string]int, len(committed))
	for _, id := range committed {
		seen[id]++
	}
	if got, want := len(seen), descriptorCount; got != want {
		t.Fatalf("committed %d unique generations, want %d", got, want)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("generation %q committed %d times, want exactly once", id, count)
		}
	}
	for _, work := range works {
		if got, want := loader.callCount(work), 1; got != want {
			t.Errorf("loader called %d times for %s, want %d (exactly-once under concurrent drain)",
				got, loaderKey(work), want)
		}
	}
}
