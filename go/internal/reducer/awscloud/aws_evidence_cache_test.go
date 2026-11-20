// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// TestAWSDriftEvidenceCacheLoadsOncePerInputVersion pins AC5 fleet sharing:
// concurrent callers on the same input version share one load (exactly-once
// per input version), while a moved version reloads (#10878 decision 2,
// IdentityEpochCache template).
func TestAWSDriftEvidenceCacheLoadsOncePerInputVersion(t *testing.T) {
	cache := NewAWSDriftEvidenceCache(64 << 20)
	var loads atomic.Int64
	load := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		loads.Add(1)
		return []cloudruntime.AddressedRow{{ARN: "arn:1"}}, 128, nil
	}
	const callers = 16
	var wg sync.WaitGroup
	results := make([][]cloudruntime.AddressedRow, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rows, _, err := cache.LoadShared(context.Background(), "scope", "gen-1", "fp-1", load)
			results[i], errs[i] = rows, err
		}(i)
	}
	wg.Wait()
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d err = %v", i, errs[i])
		}
		if len(results[i]) != 1 || results[i][0].ARN != "arn:1" {
			t.Fatalf("caller %d rows = %+v, want [arn:1]", i, results[i])
		}
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loads = %d, want exactly 1 shared load", got)
	}
	// A moved input version must reload, not serve the stale flight.
	if _, _, err := cache.LoadShared(context.Background(), "scope", "gen-1", "fp-2", load); err != nil {
		t.Fatalf("moved version err = %v", err)
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("loads after version move = %d, want 2", got)
	}
}

// TestAWSDriftEvidenceCachePassthroughOverCap pins observable passthrough:
// an oversized payload bypasses the cache and counts, never evicts peers.
func TestAWSDriftEvidenceCachePassthroughOverCap(t *testing.T) {
	cache := NewAWSDriftEvidenceCache(64)
	var loads atomic.Int64
	load := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		loads.Add(1)
		return []cloudruntime.AddressedRow{{ARN: "arn:big"}}, 1 << 20, nil
	}
	for i := 0; i < 2; i++ {
		rows, shared, err := cache.LoadShared(context.Background(), "scope", "gen-1", fmt.Sprintf("fp-%d", i), load)
		if err != nil {
			t.Fatalf("load %d err = %v", i, err)
		}
		if shared {
			t.Fatalf("load %d shared = true, want passthrough (false)", i)
		}
		if len(rows) != 1 {
			t.Fatalf("load %d rows = %d, want 1", i, len(rows))
		}
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("loads = %d, want 2 (no caching over cap)", got)
	}
	if got := cache.PassthroughCount(); got != 2 {
		t.Fatalf("passthrough = %d, want 2", got)
	}
}

// TestAWSDriftEvidenceCacheEvictsOldestOverBudget pins bounded memory:
// stored flights share one total budget; when a new flight would exceed it
// the oldest stored flight evicts and the next caller for the evicted key
// reloads (#10878 review P2: flights must never accumulate unboundedly).
func TestAWSDriftEvidenceCacheEvictsOldestOverBudget(t *testing.T) {
	cache := NewAWSDriftEvidenceCache(256)
	var loads atomic.Int64
	load := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		n := loads.Add(1)
		return []cloudruntime.AddressedRow{{ARN: fmt.Sprintf("arn:%d", n)}}, 128, nil
	}
	ctx := context.Background()
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("load fp-1 err = %v", err)
	}
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-2", load); err != nil {
		t.Fatalf("load fp-2 err = %v", err)
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("loads = %d, want 2", got)
	}
	// A repeat caller for a stored key shares without reloading.
	if rows, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("reload fp-1 err = %v", err)
	} else if !shared {
		t.Fatalf("repeat fp-1 shared = false, want true")
	} else if len(rows) != 1 || rows[0].ARN != "arn:1" {
		t.Fatalf("repeat fp-1 rows = %+v, want [arn:1]", rows)
	}
	// A third version over budget evicts the oldest stored flight (fp-1).
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-3", load); err != nil {
		t.Fatalf("load fp-3 err = %v", err)
	}
	rows, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load)
	if err != nil {
		t.Fatalf("post-eviction fp-1 err = %v", err)
	}
	if shared {
		t.Fatalf("post-eviction fp-1 shared = true, want reload after oldest-first eviction")
	}
	if got := loads.Load(); got != 4 {
		t.Fatalf("loads = %d, want 4 (fp-1 reloaded after eviction)", got)
	}
	if len(rows) != 1 || rows[0].ARN != "arn:4" {
		t.Fatalf("post-eviction fp-1 rows = %+v, want [arn:4]", rows)
	}
}

// TestAWSDriftEvidenceCacheExpiresEntriesPastTTL pins time-bounded
// staleness: a stored flight older than the TTL serves nobody and the next
// caller reloads. A zero TTL disables expiry (budget eviction still bounds
// memory).
func TestAWSDriftEvidenceCacheExpiresEntriesPastTTL(t *testing.T) {
	now := time.Now()
	cache := NewAWSDriftEvidenceCache(1<<20,
		WithAWSDriftEvidenceCacheTTL(time.Minute, func() time.Time { return now }))
	var loads atomic.Int64
	load := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		loads.Add(1)
		return []cloudruntime.AddressedRow{{ARN: "arn:ttl"}}, 128, nil
	}
	ctx := context.Background()
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("load err = %v", err)
	}
	if _, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("repeat err = %v", err)
	} else if !shared {
		t.Fatalf("repeat shared = false, want true before TTL")
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loads = %d, want 1 before TTL", got)
	}
	now = now.Add(2 * time.Minute)
	if _, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("post-TTL err = %v", err)
	} else if shared {
		t.Fatalf("post-TTL shared = true, want reload after expiry")
	}
	if got := loads.Load(); got != 2 {
		t.Fatalf("loads = %d, want 2 after TTL expiry", got)
	}
}
