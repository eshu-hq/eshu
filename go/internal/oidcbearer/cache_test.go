// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"testing"
	"time"
)

// waitForRebuild polls the resolver's snapshot until builtAt advances past
// after, or fails the test after a bounded number of attempts. Rebuilds run
// on a background goroutine (cache.go's triggerRebuild), so tests that
// advance a fake clock past the TTL must wait for that goroutine to finish
// rather than asserting immediately.
func waitForRebuild(t *testing.T, c *cache, after time.Time) *snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := c.ptr.Load()
		if snap != nil && snap.builtAt.After(after) {
			return snap
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for background rebuild to complete")
	return nil
}

// TestCacheReflectsProviderCRUDAfterTTL proves provider CRUD (here: removing
// a provider) becomes visible once the cache's TTL elapses, without a
// process restart — the mechanism issue #5162's AC #5 requires.
func TestCacheReflectsProviderCRUDAfterTTL(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	calls := 0
	source := &fakeProviderSource{providers: []BearerProvider{testProvider()}}
	fakeNow := time.Now()
	nowFn := func() time.Time { return fakeNow }

	resolver, err := NewResolver(context.Background(), Config{
		Source:          source,
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		TTL:             time.Millisecond,
		Now:             nowFn,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	initial := resolver.cache.ptr.Load()
	if initial.empty() {
		t.Fatal("initial snapshot is empty, want the one seeded provider")
	}

	// CRUD: the provider is disabled/removed.
	source.providers = nil
	// Advance the fake clock past the TTL so the next read observes
	// staleness and triggers exactly one background rebuild.
	fakeNow = fakeNow.Add(time.Hour)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	// This call only triggers the rebuild; it still uses the (stale but not
	// yet replaced) snapshot for its own verdict, per the "rebuild off the
	// read path" design — so it may still succeed once more here.
	_, _, _ = resolver.ResolveScopedToken(context.Background(), token)

	rebuilt := waitForRebuild(t, resolver.cache, initial.builtAt)
	if !rebuilt.empty() {
		t.Fatal("rebuilt snapshot still has providers, want empty after the CRUD removal")
	}

	// A subsequent resolve against the now-empty snapshot must fall through
	// instantly rather than deny.
	_, ok, resolveErr := resolver.ResolveScopedToken(context.Background(), token)
	if ok || resolveErr != nil {
		t.Fatalf("ResolveScopedToken() after CRUD removal = ok:%v err:%v, want (false, nil) fall-through", ok, resolveErr)
	}
}

// TestCacheReusesVerifierWhenRevisionUnchanged proves a rebuild triggered by
// TTL expiry, with the provider's (IssuerURL, RevisionID) unchanged, reuses
// the existing verifier rather than calling VerifierFactory again — the
// "no needless JWKS/discovery traffic for a stable provider set" invariant.
func TestCacheReusesVerifierWhenRevisionUnchanged(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	calls := 0
	provider := testProvider()
	source := &fakeProviderSource{providers: []BearerProvider{provider}}
	fakeNow := time.Now()
	nowFn := func() time.Time { return fakeNow }

	resolver, err := NewResolver(context.Background(), Config{
		Source:          source,
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		TTL:             time.Millisecond,
		Now:             nowFn,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("factory calls after construction = %d, want 1", calls)
	}

	initial := resolver.cache.ptr.Load()
	fakeNow = fakeNow.Add(time.Hour)
	// Trigger a rebuild via the lazy TTL check (no provider change: same
	// IssuerURL and RevisionID).
	resolver.cache.triggerRebuild()
	rebuilt := waitForRebuild(t, resolver.cache, initial.builtAt)

	if calls != 1 {
		t.Fatalf("factory calls after unchanged-revision rebuild = %d, want still 1 (verifier reused)", calls)
	}
	reusedEntry, ok := rebuilt.byProviderConfigID[provider.ProviderConfigID]
	if !ok {
		t.Fatal("rebuilt snapshot missing the provider entry")
	}
	initialEntry := initial.byProviderConfigID[provider.ProviderConfigID]
	if reusedEntry.verifier != initialEntry.verifier {
		t.Fatal("rebuild replaced the verifier pointer despite an unchanged (IssuerURL, RevisionID)")
	}
}

// TestCacheRebuildsVerifierWhenRevisionChanges proves the opposite: a
// changed RevisionID (a provider config update) forces a fresh verifier
// build, not a stale reused one.
func TestCacheRebuildsVerifierWhenRevisionChanges(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	calls := 0
	provider := testProvider()
	source := &fakeProviderSource{providers: []BearerProvider{provider}}
	fakeNow := time.Now()
	nowFn := func() time.Time { return fakeNow }

	resolver, err := NewResolver(context.Background(), Config{
		Source:          source,
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		TTL:             time.Millisecond,
		Now:             nowFn,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	initial := resolver.cache.ptr.Load()
	provider.RevisionID = "rev_2"
	source.providers = []BearerProvider{provider}
	fakeNow = fakeNow.Add(time.Hour)
	resolver.cache.triggerRebuild()
	waitForRebuild(t, resolver.cache, initial.builtAt)

	if calls != 2 {
		t.Fatalf("factory calls after revision change = %d, want 2 (rebuilt once more)", calls)
	}
}

// TestNewResolverRejectsMissingDependencies proves NewResolver fails closed
// with a specific error rather than constructing a Resolver that panics or
// silently no-ops later; wiring must treat "no audience configured" as "do
// not construct a Resolver at all", per the package README's activation
// contract.
func TestNewResolverRejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	base := Config{
		Source:        &fakeProviderSource{},
		GrantResolver: testGrantResolver(),
		Audience:      testAudience,
	}

	missingSource := base
	missingSource.Source = nil
	if _, err := NewResolver(context.Background(), missingSource); err != ErrProviderSourceRequired {
		t.Fatalf("NewResolver() error = %v, want ErrProviderSourceRequired", err)
	}

	missingGrantResolver := base
	missingGrantResolver.GrantResolver = nil
	if _, err := NewResolver(context.Background(), missingGrantResolver); err != ErrGrantResolverRequired {
		t.Fatalf("NewResolver() error = %v, want ErrGrantResolverRequired", err)
	}

	missingAudience := base
	missingAudience.Audience = ""
	if _, err := NewResolver(context.Background(), missingAudience); err != ErrAudienceRequired {
		t.Fatalf("NewResolver() error = %v, want ErrAudienceRequired", err)
	}
}
