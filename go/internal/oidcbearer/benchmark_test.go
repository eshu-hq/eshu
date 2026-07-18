// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"testing"
	"time"
)

// BenchmarkResolveScopedToken_EmptySnapshot proves AC #4: a deployment with
// zero enabled bearer IdPs pays no per-request cost. The clock is held fixed
// so the TTL staleness check never fires mid-benchmark (which would spawn a
// background rebuild goroutine and pollute the allocation count with
// something that is not part of the actual per-request hot path).
func BenchmarkResolveScopedToken_EmptySnapshot(b *testing.B) {
	fixed := time.Now()
	idp := newTestIdP(b)
	calls := 0
	resolver, err := NewResolver(context.Background(), Config{
		Source:          &fakeProviderSource{},
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		Now:             func() time.Time { return fixed },
	})
	if err != nil {
		b.Fatalf("NewResolver() error = %v", err)
	}

	ctx := context.Background()
	credential := "aaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbb.cccccccccccccccccccccccccccc"

	allocs := testing.AllocsPerRun(100, func() {
		_, _, _ = resolver.ResolveScopedToken(ctx, credential)
	})
	if allocs != 0 {
		b.Fatalf("AllocsPerRun = %v, want 0 for the zero-provider fast path", allocs)
	}
	if calls != 0 {
		b.Fatalf("verifier factory calls = %d, want 0 for the zero-provider fast path", calls)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = resolver.ResolveScopedToken(ctx, credential)
	}
}
