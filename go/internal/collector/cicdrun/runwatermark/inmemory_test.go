// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runwatermark

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestInMemoryStoreLoadMissReturnsNotFound(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	_, ok, err := store.Load(context.Background(), Key{ScopeID: "scope-1", Repository: "octo/repo"})
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if ok {
		t.Fatalf("Load() ok = true, want false for empty store")
	}
}

func TestInMemoryStoreSaveThenLoadRoundTrips(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	want := Watermark{Key: key, LastRunID: "100", GenerationID: "gen-1", FencingToken: 1}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	got, ok, err := store.Load(context.Background(), key)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("Load() ok = false, want true")
	}
	if got.LastRunID != want.LastRunID || got.FencingToken != want.FencingToken {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestInMemoryStoreSaveRejectsOlderFence proves the fencing invariant: a
// superseded claim (lower fencing token) cannot regress a watermark a newer
// claim already advanced. This is the store-level half of the stale-claim
// matrix required by concurrency-deadlock-rigor.
func TestInMemoryStoreSaveRejectsOlderFence(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	if err := store.Save(context.Background(), Watermark{
		Key: key, LastRunID: "200", GenerationID: "gen-2", FencingToken: 5,
	}); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}

	err := store.Save(context.Background(), Watermark{
		Key: key, LastRunID: "150", GenerationID: "gen-1", FencingToken: 3,
	})
	if !errors.Is(err, ErrStaleFence) {
		t.Fatalf("Save() error = %v, want ErrStaleFence", err)
	}

	got, ok, loadErr := store.Load(context.Background(), key)
	if loadErr != nil {
		t.Fatalf("Load() error = %v, want nil", loadErr)
	}
	if !ok || got.LastRunID != "200" || got.FencingToken != 5 {
		t.Fatalf("Load() = %+v, ok=%v; watermark must remain at the higher fencing token's value", got, ok)
	}
}

// TestInMemoryStoreSaveAllowsEqualFenceIdempotentRetry proves a retried
// claim carrying the SAME fencing token as the stored row (a duplicate
// delivery / idempotent re-fetch of an already-succeeded claim) is accepted
// rather than rejected, matching the Postgres checkpoint pattern's `<=`
// guard.
func TestInMemoryStoreSaveAllowsEqualFenceIdempotentRetry(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	first := Watermark{Key: key, LastRunID: "200", GenerationID: "gen-2", FencingToken: 5}
	if err := store.Save(context.Background(), first); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	retry := Watermark{Key: key, LastRunID: "200", GenerationID: "gen-2", FencingToken: 5}
	if err := store.Save(context.Background(), retry); err != nil {
		t.Fatalf("Save() retry error = %v, want nil (idempotent re-delivery must succeed)", err)
	}
}

// TestInMemoryStoreSaveAdvancesOnHigherFence proves ordering: a later claim
// with a strictly higher fencing token always advances the watermark.
func TestInMemoryStoreSaveAdvancesOnHigherFence(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	if err := store.Save(context.Background(), Watermark{
		Key: key, LastRunID: "100", GenerationID: "gen-1", FencingToken: 1,
	}); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	if err := store.Save(context.Background(), Watermark{
		Key: key, LastRunID: "300", GenerationID: "gen-3", FencingToken: 9,
	}); err != nil {
		t.Fatalf("Save() error = %v, want nil", err)
	}
	got, ok, err := store.Load(context.Background(), key)
	if err != nil || !ok {
		t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
	}
	if got.LastRunID != "300" || got.FencingToken != 9 {
		t.Fatalf("Load() = %+v, want LastRunID=300 FencingToken=9", got)
	}
}

func TestInMemoryStoreLoadRejectsInvalidKey(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if _, _, err := store.Load(context.Background(), Key{}); err == nil {
		t.Fatalf("Load() error = nil, want error for invalid key")
	}
}

func TestInMemoryStoreSaveRejectsInvalidWatermark(t *testing.T) {
	t.Parallel()

	store := NewInMemoryStore()
	if err := store.Save(context.Background(), Watermark{}); err == nil {
		t.Fatalf("Save() error = nil, want error for invalid watermark")
	}
}

// TestInMemoryStoreConcurrentSavesAreSerialized proves concurrent claim
// workers writing to the SAME key (the conflict domain) do not corrupt
// store state or race: the mutex-guarded map must end at exactly one
// consistent winner, and the winner must be the highest fencing token
// observed, regardless of goroutine interleaving.
func TestInMemoryStoreConcurrentSavesAreSerialized(t *testing.T) {
	store := NewInMemoryStore()
	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}

	const workers = 50
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 1; i <= workers; i++ {
		fencingToken := int64(i)
		go func() {
			defer wg.Done()
			_ = store.Save(context.Background(), Watermark{
				Key:          key,
				LastRunID:    "1",
				GenerationID: "gen",
				FencingToken: fencingToken,
			})
		}()
	}
	wg.Wait()

	got, ok, err := store.Load(context.Background(), key)
	if err != nil || !ok {
		t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
	}
	if got.FencingToken != workers {
		t.Fatalf("Load().FencingToken = %d, want %d (highest fencing token must win)", got.FencingToken, workers)
	}
}

// TestInMemoryStoreSaveGuard pins the guard the Postgres store's conflict filter
// states in SQL: the fence decides inside one generation, because tokens count
// claims of one work item, and the run ID decides across generations (#10063).
func TestInMemoryStoreSaveGuard(t *testing.T) {
	t.Parallel()

	key := Key{ScopeID: "scope-1", Repository: "octo/repo"}
	stored := Watermark{Key: key, LastRunID: "200", GenerationID: "gen-1", FencingToken: 2}
	tests := []struct {
		name    string
		write   Watermark
		wantErr error
		wantRun string
	}{
		{
			name:    "same generation, older fence, newer run is a superseded claim",
			write:   Watermark{Key: key, LastRunID: "300", GenerationID: "gen-1", FencingToken: 1},
			wantErr: ErrStaleFence,
			wantRun: "200",
		},
		{
			name:    "same generation, equal fence is a redelivery",
			write:   Watermark{Key: key, LastRunID: "200", GenerationID: "gen-1", FencingToken: 2},
			wantRun: "200",
		},
		{
			name:    "next generation, first attempt after a retried claim advances",
			write:   Watermark{Key: key, LastRunID: "300", GenerationID: "gen-2", FencingToken: 1},
			wantRun: "300",
		},
		{
			name:    "other generation, same run advances",
			write:   Watermark{Key: key, LastRunID: "200", GenerationID: "gen-2", FencingToken: 1},
			wantRun: "200",
		},
		{
			name:    "other generation, older run cannot regress the row despite a higher fence",
			write:   Watermark{Key: key, LastRunID: "150", GenerationID: "gen-0", FencingToken: 9},
			wantErr: ErrStaleFence,
			wantRun: "200",
		},
		{
			name:    "run ids order numerically, not as text",
			write:   Watermark{Key: key, LastRunID: "1000", GenerationID: "gen-2", FencingToken: 1},
			wantRun: "1000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := NewInMemoryStore()
			if err := store.Save(context.Background(), stored); err != nil {
				t.Fatalf("seed Save() error = %v", err)
			}
			if err := store.Save(context.Background(), tc.write); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Save() error = %v, want %v", err, tc.wantErr)
			}
			got, ok, err := store.Load(context.Background(), key)
			if err != nil || !ok {
				t.Fatalf("Load() = %+v, %v, %v", got, ok, err)
			}
			if got.LastRunID != tc.wantRun {
				t.Fatalf("LastRunID = %q, want %q", got.LastRunID, tc.wantRun)
			}
		})
	}
}
