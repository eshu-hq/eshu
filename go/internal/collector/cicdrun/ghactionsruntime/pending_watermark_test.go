// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/cicdrun/runwatermark"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// This file proves pendingWatermarks (#5429) is safe under the concurrency
// collector.MultiSourceCollectorHost actually exercises: several
// ClaimedService workers sharing ONE registered ClaimedSource value, each
// claiming and processing a DIFFERENT work item at the same time (see
// pending_watermark.go's pendingWatermarks doc comment for why per-key
// races are additionally ruled out by claim-level exclusivity). Run with
// `-race`.

// TestPendingWatermarksConcurrentStashAndTakeIsRaceFree drives many
// goroutines, each owning its own (ScopeID, GenerationID) key, concurrently
// stashing and then taking their own entry. It never touches ClaimedSource
// or NextClaimed -- it isolates the pendingWatermarks map itself as the unit
// under race-detector scrutiny.
func TestPendingWatermarksConcurrentStashAndTakeIsRaceFree(t *testing.T) {
	t.Parallel()

	pending := newPendingWatermarks()
	const workers = 64
	const iterationsPerWorker = 25

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterationsPerWorker; i++ {
				item := workflow.WorkItem{
					ScopeID:      fmt.Sprintf("scope-%d", worker),
					GenerationID: fmt.Sprintf("generation-%d-%d", worker, i),
				}
				target := TargetConfig{Repository: fmt.Sprintf("owner/repo-%d", worker)}
				wantRunID := fmt.Sprintf("run-%d-%d", worker, i)
				pending.stash(item, target, wantRunID)
				entry, ok := pending.takeFor(item)
				if !ok {
					t.Errorf("takeFor(worker=%d, i=%d) ok = false, want true", worker, i)
					continue
				}
				if entry.newestRunID != wantRunID {
					t.Errorf("takeFor(worker=%d, i=%d) newestRunID = %q, want %q", worker, i, entry.newestRunID, wantRunID)
				}
				if entry.target.Repository != target.Repository {
					t.Errorf("takeFor(worker=%d, i=%d) target.Repository = %q, want %q",
						worker, i, entry.target.Repository, target.Repository)
				}
				// A key must never be observable twice: this worker's own
				// prior take already removed it.
				if _, ok := pending.takeFor(item); ok {
					t.Errorf("takeFor(worker=%d, i=%d) [second take] ok = true, want false (entry must be consumed once)", worker, i)
				}
			}
		}(w)
	}
	wg.Wait()
}

// TestClaimedSourceConcurrentNextClaimedAndObserveIsRaceFree drives the
// public ClaimedSource surface concurrently: many goroutines each run
// NextClaimed followed by ObserveClaimedGenerationCommitted for their OWN
// work item against their OWN configured target (distinct ScopeID and
// Repository per goroutine), the exact pattern
// collector.MultiSourceCollectorHost produces when Workers > 1 share one
// registered source across DIFFERENT dispatched work items. Each goroutine's
// observed watermark must end up durably saved at ITS OWN newest run ID,
// with no cross-talk between goroutines. Run with `-race`.
//
// Every goroutine uses its own target deliberately: two goroutines racing
// Save calls against the SAME watermark key would correctly trigger
// runwatermark.ErrStaleFence for whichever one is scheduled second (that is
// the fencing guard working as designed, proven separately by
// TestClaimedSourcePropagatesStaleWatermarkFence) -- and per
// pendingWatermarks' doc comment, claim-level exclusivity means production
// never presents the SAME key to concurrent workers in the first place.
func TestClaimedSourceConcurrentNextClaimedAndObserveIsRaceFree(t *testing.T) {
	t.Parallel()

	const workers = 32
	store := runwatermark.NewInMemoryStore()
	targets := make([]TargetConfig, workers)
	for w := 0; w < workers; w++ {
		repository := fmt.Sprintf("example/repo-%d", w)
		targets[w] = TargetConfig{
			ScopeID:             fmt.Sprintf("ci-cd:github-actions:%s", repository),
			Repository:          repository,
			Token:               "token",
			AllowedRepositories: []string{repository},
			MaxRuns:             10,
			MaxJobs:             10,
			MaxArtifacts:        10,
		}
	}
	source, err := NewClaimedSource(SourceConfig{
		CollectorInstanceID: "ci-cd-primary",
		Client:              &fixedPageClient{},
		Watermarks:          store,
		Targets:             targets,
	})
	if err != nil {
		t.Fatalf("NewClaimedSource() error = %v, want nil", err)
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(worker int) {
			defer wg.Done()
			item := workflow.WorkItem{
				CollectorKind:       scope.CollectorCICDRun,
				CollectorInstanceID: "ci-cd-primary",
				ScopeID:             targets[worker].ScopeID,
				GenerationID:        fmt.Sprintf("generation-concurrent-%d", worker),
				CurrentFencingToken: 1,
			}
			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err != nil || !ok {
				t.Errorf("NextClaimed(worker=%d) = _, %v, %v, want ok=true and nil error", worker, ok, err)
				return
			}
			for range collected.Facts {
				// drain without asserting shape: this test's subject is
				// concurrency safety, not fact content.
			}
			if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
				t.Errorf("ObserveClaimedGenerationCommitted(worker=%d) error = %v, want nil", worker, err)
			}
		}(w)
	}
	wg.Wait()

	for w := 0; w < workers; w++ {
		got, ok, loadErr := store.Load(context.Background(), watermarkKey(targets[w]))
		if loadErr != nil || !ok {
			t.Errorf("Load(worker=%d) = %+v, %v, %v, want a stored watermark", w, got, ok, loadErr)
			continue
		}
		if got.LastRunID != "999" {
			t.Errorf("Load(worker=%d) LastRunID = %q, want %q", w, got.LastRunID, "999")
		}
	}
}

// fixedPageClient always returns the same one-run page, independent of
// target. It exists so concurrency tests can drive many concurrent claims
// against one shared ClaimedSource without needing distinct fetch results
// per call.
type fixedPageClient struct{}

func (fixedPageClient) FetchRuns(context.Context, TargetConfig) (RunPage, error) {
	return RunPage{Snapshots: []RunSnapshot{minimalRunSnapshot("999")}}, nil
}
