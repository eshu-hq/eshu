// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestRepositoryReindexStoreWatermarksLive proves the per-repository reindex
// watermark contract (#7620) against real Postgres:
//
//  1. One request stores one row per distinct scope and returns each stored
//     value, stamped from the database clock and ordered by scope ID.
//  2. Concurrent requests with overlapping scope sets, named in opposite
//     orders, all succeed (no SQLSTATE 40P01 deadlock), and every stored value
//     equals the largest value any request returned for that scope.
//  3. A stored value ahead of the database clock is never moved backward.
//  4. The per-cycle read returns only rows newer than the fleet watermark.
//
// It runs in the live-postgres-readiness runner. Run locally with a disposable
// PostgreSQL 18 administrative database:
//
//	ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/maintenance -run RepositoryReindexStoreWatermarksLive -count=1
func TestRepositoryReindexStoreWatermarksLive(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	optIn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE")
	ctx, sqlDB := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}
	store := maintenancestore.NewRepositoryReindexStore(postgres.SQLDB{DB: sqlDB})

	first, err := store.RequestRepositoryReindex(ctx, []string{"scope:b", "scope:a", "scope:b"})
	if err != nil {
		t.Fatalf("first RequestRepositoryReindex() error = %v", err)
	}
	if len(first) != 2 || first[0].ScopeID != "scope:a" || first[1].ScopeID != "scope:b" {
		t.Fatalf("first request = %+v, want scope:a then scope:b", first)
	}
	again, err := store.RequestRepositoryReindex(ctx, []string{"scope:a"})
	if err != nil {
		t.Fatalf("second RequestRepositoryReindex() error = %v", err)
	}
	if again[0].RequestedAt.Before(first[0].RequestedAt) {
		t.Fatalf("second watermark %v before first %v", again[0].RequestedAt, first[0].RequestedAt)
	}

	maxByScope := requestOverlappingConcurrently(ctx, t, store, 16)
	stored, err := store.RepositoryReindexWatermarks(ctx, time.Time{})
	if err != nil {
		t.Fatalf("RepositoryReindexWatermarks() error = %v", err)
	}
	for scopeID, maxReturned := range maxByScope {
		if !stored[scopeID].Equal(maxReturned) {
			t.Fatalf("stored watermark for %s = %v, want max returned %v", scopeID, stored[scopeID], maxReturned)
		}
	}

	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE repository_reindex_requests SET requested_at = $2 WHERE scope_id = $1`, "scope:a", future); err != nil {
		t.Fatalf("seed future watermark: %v", err)
	}
	afterFuture, err := store.RequestRepositoryReindex(ctx, []string{"scope:a"})
	if err != nil {
		t.Fatalf("RequestRepositoryReindex() after future seed error = %v", err)
	}
	if !afterFuture[0].RequestedAt.Equal(future) {
		t.Fatalf("watermark after future seed = %v, want unchanged %v", afterFuture[0].RequestedAt, future)
	}

	fleet := time.Now().UTC().Add(30 * time.Minute)
	newer, err := store.RepositoryReindexWatermarks(ctx, fleet)
	if err != nil {
		t.Fatalf("RepositoryReindexWatermarks(fleet) error = %v", err)
	}
	if len(newer) != 1 || !newer["scope:a"].Equal(future) {
		t.Fatalf("watermarks newer than fleet = %v, want only scope:a at %v", newer, future)
	}
}

// requestOverlappingConcurrently runs workers concurrent requests: even
// workers name scopes c:001..c:100 ascending, odd workers c:150..c:051
// descending, so their row sets overlap on c:051..c:100. It returns the
// largest watermark returned for each scope.
func requestOverlappingConcurrently(
	ctx context.Context,
	t *testing.T,
	store maintenancestore.RepositoryReindexStore,
	workers int,
) map[string]time.Time {
	t.Helper()
	ascending := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		ascending = append(ascending, fmt.Sprintf("c:%03d", i))
	}
	descending := make([]string, 0, 100)
	for i := 150; i >= 51; i-- {
		descending = append(descending, fmt.Sprintf("c:%03d", i))
	}

	var (
		mu         sync.Mutex
		maxByScope = make(map[string]time.Time)
		errs       = make([]error, workers)
		wg         sync.WaitGroup
	)
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			scopeIDs := ascending
			if i%2 == 1 {
				scopeIDs = descending
			}
			for range 10 {
				requests, err := store.RequestRepositoryReindex(ctx, scopeIDs)
				if err != nil {
					errs[i] = err
					return
				}
				mu.Lock()
				for _, request := range requests {
					if request.RequestedAt.After(maxByScope[request.ScopeID]) {
						maxByScope[request.ScopeID] = request.RequestedAt
					}
				}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent RequestRepositoryReindex()[%d] error = %v", i, err)
		}
	}
	if got, want := len(maxByScope), 150; got != want {
		t.Fatalf("concurrent requests touched %d scopes, want %d", got, want)
	}
	return maxByScope
}
