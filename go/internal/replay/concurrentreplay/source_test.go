// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package concurrentreplay_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/go/internal/replay/concurrentreplay"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// writeCassetteFile persists f as a temp JSON cassette and returns its path.
// Mirrors the construction path proven in cassette/source_test.go: cassette.File
// -> json.MarshalIndent -> os.WriteFile -> cassette.NewSource(path).
func writeCassetteFile(t *testing.T, f cassette.File) string {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshal cassette: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cassette.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write cassette file: %v", err)
	}
	return path
}

// scopeFact builds one minimal scope with one fact for the given generation.
func scopeFact(scopeID, generationID string, observedAt time.Time) cassette.Scope {
	return cassette.Scope{
		ScopeID:       scopeID,
		SourceSystem:  "kubernetes_live",
		ScopeKind:     "cluster",
		CollectorKind: "kubernetes_live",
		GenerationID:  generationID,
		ObservedAt:    observedAt,
		Facts: []cassette.Fact{
			{
				FactKind:      "kubernetes_live.pod_template",
				StableFactKey: scopeID + ":deployment:default:demo",
				SchemaVersion: "1.0.0",
				Payload:       map[string]any{"name": "demo"},
			},
		},
	}
}

// multiScopeCassette builds a cassette with n distinct scopes, each with its own
// generation, so a concurrent drain can be checked for exactly-once delivery per
// generation.
func multiScopeCassette(n int, observedAt time.Time) cassette.File {
	scopes := make([]cassette.Scope, 0, n)
	for i := 0; i < n; i++ {
		id := "kubernetes_live:cluster:cluster-" + string(rune('a'+i))
		gen := "gen-cluster-" + string(rune('a'+i))
		scopes = append(scopes, scopeFact(id, gen, observedAt))
	}
	return cassette.File{
		Collector:     "kubernetes_live",
		SchemaVersion: "1",
		Scopes:        scopes,
	}
}

// TestSourceConcurrentNextDeliversEachGenerationExactlyOnce proves that N
// goroutines calling Next concurrently against a Source wrapping a real
// cassette.Source receive the full set of recorded generations, each exactly
// once, with no duplicates and none missing. Must be run with -race: the
// delegate (cassette.Source) is not safe for concurrent use on its own, so this
// is the proof that concurrentreplay.Source's mutex makes it safe.
func TestSourceConcurrentNextDeliversEachGenerationExactlyOnce(t *testing.T) {
	t.Parallel()

	const scopeCount = 5
	const workerCount = 8

	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, multiScopeCassette(scopeCount, observedAt))
	delegate, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	src := concurrentreplay.NewSource(delegate)

	var (
		mu        sync.Mutex
		delivered []string
	)
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for {
				gen, ok, err := src.Next(context.Background())
				if err != nil {
					t.Errorf("Next: unexpected error %v", err)
					return
				}
				if !ok {
					return
				}
				mu.Lock()
				delivered = append(delivered, gen.Generation.GenerationID)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got, want := len(delivered), scopeCount; got != want {
		t.Fatalf("delivered %d generations, want exactly %d (no dup/missing)", got, want)
	}
	sort.Strings(delivered)
	seen := make(map[string]int, len(delivered))
	for _, id := range delivered {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("generation %q delivered %d times, want exactly once", id, count)
		}
	}
	if !src.Drained() {
		t.Error("Drained() = false after concurrent drain, want true")
	}
	if got, want := src.Served(), scopeCount; got != want {
		t.Errorf("Served() = %d, want %d", got, want)
	}
}

// TestSourceLatchesDrainDespiteDelegateRestart proves the one-shot drain latch
// converts the delegate's poll-restart semantics (cassette.Source resets its
// scopeIndex to 0 and replays after the first ok=false) into exactly-once tape
// delivery: once Source observes ok=false from the delegate, every subsequent
// Next call must return ok=false forever, never re-invoking the delegate to
// discover its restarted batch.
func TestSourceLatchesDrainDespiteDelegateRestart(t *testing.T) {
	t.Parallel()

	const scopeCount = 3
	observedAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	path := writeCassetteFile(t, multiScopeCassette(scopeCount, observedAt))
	delegate, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource: %v", err)
	}
	src := concurrentreplay.NewSource(delegate)

	for i := 0; i < scopeCount; i++ {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("Next[%d]: unexpected error %v", i, err)
		}
		if !ok {
			t.Fatalf("Next[%d]: ok=false, want true (scope %d of %d)", i, i, scopeCount)
		}
		if gen.Generation.GenerationID == "" {
			t.Fatalf("Next[%d]: empty GenerationID", i)
		}
	}

	// The delegate is now exhausted. Without the latch, the underlying
	// cassette.Source would reset scopeIndex to 0 on the call after the first
	// ok=false and replay the tape again on the call after that. Prove the
	// latch defeats this: every subsequent call, well past the delegate's own
	// restart point, must return ok=false.
	for i := 0; i < scopeCount+5; i++ {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("post-drain Next[%d]: unexpected error %v", i, err)
		}
		if ok {
			t.Fatalf("post-drain Next[%d]: ok=true (generation %q), want false forever after drain",
				i, gen.Generation.GenerationID)
		}
	}

	if !src.Drained() {
		t.Error("Drained() = false, want true after full drain")
	}
	if got, want := src.Served(), scopeCount; got != want {
		t.Errorf("Served() = %d, want %d", got, want)
	}
}

// fakeErroringSource is a minimal collector.Source that yields k successful
// generations then returns an error forever after, and counts how many times
// Next was invoked so the test can prove the latch stops calling the delegate
// once it has failed.
type fakeErroringSource struct {
	mu        sync.Mutex
	succeed   int
	calls     int
	served    int
	returnErr error
}

func (f *fakeErroringSource) Next(_ context.Context) (collector.CollectedGeneration, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.served < f.succeed {
		f.served++
		return collector.CollectedGeneration{
			Scope:      scope.IngestionScope{ScopeID: "fake-scope"},
			Generation: scope.ScopeGeneration{GenerationID: "fake-gen"},
		}, true, nil
	}
	return collector.CollectedGeneration{}, false, f.returnErr
}

func (f *fakeErroringSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestSourceLatchesOnDelegateError proves that once the delegate returns an
// error, Source surfaces it exactly once and every subsequent Next call
// returns ok=false, err=nil without re-invoking the delegate — the same
// one-shot latch that defeats poll-restart also stops the delegate from being
// asked again after it has already told the driver it failed.
func TestSourceLatchesOnDelegateError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom: fake delegate failure")
	fake := &fakeErroringSource{succeed: 2, returnErr: wantErr}
	src := concurrentreplay.NewSource(fake)

	for i := 0; i < fake.succeed; i++ {
		_, ok, err := src.Next(context.Background())
		if err != nil {
			t.Fatalf("Next[%d]: unexpected error %v", i, err)
		}
		if !ok {
			t.Fatalf("Next[%d]: ok=false, want true", i)
		}
	}

	// This call should surface the delegate's error exactly once.
	_, ok, err := src.Next(context.Background())
	if ok {
		t.Fatal("Next after delegate error: ok=true, want false")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("Next after delegate error: err=%v, want it to wrap %v", err, wantErr)
	}

	callsAfterError := fake.callCount()

	// Every subsequent call must return drained (ok=false, err=nil) without
	// re-invoking the delegate.
	for i := 0; i < 5; i++ {
		_, ok, err := src.Next(context.Background())
		if ok {
			t.Fatalf("post-error Next[%d]: ok=true, want false", i)
		}
		if err != nil {
			t.Fatalf("post-error Next[%d]: err=%v, want nil (error surfaces once)", i, err)
		}
	}

	if got := fake.callCount(); got != callsAfterError {
		t.Errorf("delegate.Next called %d more times after latch, want 0 (calls stayed at %d)",
			got-callsAfterError, callsAfterError)
	}
	if !src.Drained() {
		t.Error("Drained() = false, want true after delegate error")
	}
	if got, want := src.Served(), fake.succeed; got != want {
		t.Errorf("Served() = %d, want %d", got, want)
	}
}
