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

// TestCacheFailsClosedOnDuplicateIssuer proves that when two active providers
// (two tenants sharing one corporate IdP, say) claim the same issuer URL, the
// cache excludes that issuer from the snapshot entirely instead of letting the
// last-processed row silently own it. Provider-config uniqueness is scoped to
// tenant/kind/key, not issuer, so this collision is a legitimate config state,
// and a token routed by `iss` alone cannot say which tenant it belongs to.
// Picking either row would authenticate the caller against that row's
// TenantID/WorkspaceID/grants — a cross-tenant escalation. Fail closed: a valid
// token from the shared issuer is denied as an unknown issuer, never resolved.
func TestCacheFailsClosedOnDuplicateIssuer(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	calls := 0

	tenantA := testProvider()
	tenantB := testProvider()
	tenantB.ProviderConfigID = "pc_tenant_b"
	tenantB.TenantID = "tenant_b"
	// tenantB deliberately keeps tenantA's IssuerURL (testIssuer): the shared
	// corporate-IdP collision this test guards against. A third provider on its
	// own distinct issuer keeps the snapshot non-empty, so the shared-issuer
	// token exercises the explicit deny path (unknown issuer) rather than the
	// zero-provider fall-through it would take if every provider were excluded.
	const keeperIssuer = "https://unambiguous-idp.example.test"
	keeper := testProvider()
	keeper.ProviderConfigID = "pc_keeper"
	keeper.IssuerURL = keeperIssuer
	source := &fakeProviderSource{providers: []BearerProvider{tenantA, tenantB, keeper}}

	resolver, err := NewResolver(context.Background(), Config{
		Source:          source,
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		Now:             time.Now,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	snap := resolver.cache.ptr.Load()
	if _, present := snap.byIssuer[testIssuer]; present {
		t.Fatal("shared issuer present in the snapshot; want it dropped fail-closed")
	}
	if _, present := snap.byProviderConfigID[tenantA.ProviderConfigID]; present {
		t.Fatal("tenantA retained despite sharing an ambiguous issuer; want it excluded")
	}
	if _, present := snap.byProviderConfigID[tenantB.ProviderConfigID]; present {
		t.Fatal("tenantB retained despite sharing an ambiguous issuer; want it excluded")
	}
	if _, present := snap.byIssuer[keeperIssuer]; !present {
		t.Fatal("unambiguous keeper issuer dropped; want it kept")
	}

	// A valid token from the ambiguous shared issuer must be actively denied,
	// never bound to tenantA or tenantB.
	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	auth, ok, resolveErr := resolver.ResolveScopedToken(context.Background(), token)
	if ok {
		t.Fatalf("ResolveScopedToken() resolved a token for an ambiguous shared issuer (to tenant %q); want denial", auth.TenantID)
	}
	if resolveErr == nil {
		t.Fatal("ResolveScopedToken() for an ambiguous shared issuer = (false, nil) fall-through; want a deny error while other providers remain enabled")
	}
}

// TestCacheKeepsDistinctIssuersWhenOneIsDuplicated proves the exclusion is
// scoped to the offending issuer only: a third provider on its own distinct
// issuer still resolves while the duplicated pair is dropped.
func TestCacheKeepsDistinctIssuersWhenOneIsDuplicated(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	calls := 0

	dupA := testProvider()
	dupB := testProvider()
	dupB.ProviderConfigID = "pc_dup_b"
	dupB.TenantID = "tenant_b"
	const altIssuer = "https://alt-idp.example.test"
	distinct := testProvider()
	distinct.ProviderConfigID = "pc_distinct"
	distinct.IssuerURL = altIssuer

	source := &fakeProviderSource{providers: []BearerProvider{dupA, dupB, distinct}}
	resolver, err := NewResolver(context.Background(), Config{
		Source:          source,
		GrantResolver:   testGrantResolver(),
		Audience:        testAudience,
		VerifierFactory: idp.verifierFactory(&calls),
		Now:             time.Now,
	})
	if err != nil {
		t.Fatalf("NewResolver() error = %v", err)
	}

	snap := resolver.cache.ptr.Load()
	if _, present := snap.byIssuer[testIssuer]; present {
		t.Fatal("duplicated issuer present; want it dropped")
	}
	if _, present := snap.byIssuer[altIssuer]; !present {
		t.Fatal("distinct issuer dropped; want it kept alongside the excluded duplicate")
	}
}
