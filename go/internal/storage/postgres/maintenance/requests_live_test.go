// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenancestore_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// TestStatusRequestStoreRequestReindexWatermarkMonotonicLive proves the
// reindex watermark contract (#7620) against real Postgres:
//
//  1. Each request returns the stored reindex_request_requested_at, stamped
//     from the database clock, and a later request never returns an earlier
//     value.
//  2. Concurrent requests for the same ingester all succeed, and the stored
//     value equals the largest value any of them returned.
//  3. A stored value ahead of the database clock is never moved backward.
//
// It runs in the live-postgres-readiness runner, which provisions the storage
// proofs' administrative DSN. Run locally with a disposable PostgreSQL 18
// administrative database:
//
//	ESHU_GENERATION_RETENTION_PROOF_DSN=postgresql://postgres:postgres@localhost:<port>/postgres?sslmode=disable \
//	ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1 \
//	  go test ./internal/storage/postgres/maintenance -run WatermarkMonotonicLive -count=1
func TestStatusRequestStoreRequestReindexWatermarkMonotonicLive(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	optIn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE")
	ctx, sqlDB := postgresproof.OpenDisposableDatabase(t, dsn, optIn, 2*time.Minute)
	if err := postgres.ApplyBootstrap(ctx, postgres.SQLDB{DB: sqlDB}); err != nil {
		t.Fatalf("apply bootstrap schema: %v", err)
	}

	const ingester = "repository"
	store := maintenancestore.NewStatusRequestStore(postgres.SQLDB{DB: sqlDB})

	first, err := store.RequestReindex(ctx, ingester)
	if err != nil {
		t.Fatalf("first RequestReindex() error = %v", err)
	}
	second, err := store.RequestReindex(ctx, ingester)
	if err != nil {
		t.Fatalf("second RequestReindex() error = %v", err)
	}
	if second.Before(first) {
		t.Fatalf("second watermark %v before first %v", second, first)
	}

	const workers = 16
	results := make([]time.Time, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = store.RequestReindex(ctx, ingester)
		}(i)
	}
	wg.Wait()
	maxReturned := second
	for i := range workers {
		if errs[i] != nil {
			t.Fatalf("concurrent RequestReindex()[%d] error = %v", i, errs[i])
		}
		if results[i].Before(second) {
			t.Fatalf("concurrent watermark[%d] %v before earlier %v", i, results[i], second)
		}
		if results[i].After(maxReturned) {
			maxReturned = results[i]
		}
	}
	state, err := store.GetReindexState(ctx, ingester)
	if err != nil {
		t.Fatalf("GetReindexState() error = %v", err)
	}
	if !state.RequestedAt.Equal(maxReturned) {
		t.Fatalf("stored watermark %v, want max returned %v", state.RequestedAt, maxReturned)
	}

	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	if _, err := sqlDB.ExecContext(ctx,
		`UPDATE runtime_ingester_control SET reindex_request_requested_at = $2 WHERE ingester = $1`,
		ingester, future); err != nil {
		t.Fatalf("seed future watermark: %v", err)
	}
	afterFuture, err := store.RequestReindex(ctx, ingester)
	if err != nil {
		t.Fatalf("RequestReindex() after future seed error = %v", err)
	}
	if !afterFuture.Equal(future) {
		t.Fatalf("watermark after future seed = %v, want unchanged %v", afterFuture, future)
	}
}
