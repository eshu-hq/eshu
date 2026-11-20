// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

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
