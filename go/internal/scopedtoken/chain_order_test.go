// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scopedtoken

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// fakeChainResolver is a minimal query.ScopedTokenResolver stand-in for
// chain-ordering tests: it records whether it was called and returns a
// pre-configured (AuthContext, ok, error).
type fakeChainResolver struct {
	name   string
	called bool
	auth   query.AuthContext
	ok     bool
	err    error
	calls  *[]string
}

func (f *fakeChainResolver) ResolveScopedToken(context.Context, string) (query.AuthContext, bool, error) {
	f.called = true
	if f.calls != nil {
		*f.calls = append(*f.calls, f.name)
	}
	return f.auth, f.ok, f.err
}

// TestChainResolversOrderIdentityBearerFile proves issue #5162's required
// chain shape: identity (opaque hash) resolver first, then the IdP bearer
// resolver, then the file-registry resolver — matching cmd/api and
// cmd/mcp-server wiring's scopedtoken.ChainResolvers(identity, bearer, file)
// call. A resolver earlier in the chain that finds nothing (ok=false,
// err=nil) must fall through to the next; the LAST resolver to match (or
// error) determines the final result, and every resolver strictly before
// the matching one must have been consulted in order.
func TestChainResolversOrderIdentityBearerFile(t *testing.T) {
	t.Parallel()

	var calls []string
	identity := &fakeChainResolver{name: "identity", calls: &calls}
	bearer := &fakeChainResolver{name: "bearer", calls: &calls}
	file := &fakeChainResolver{
		name: "file", calls: &calls,
		auth: query.AuthContext{Mode: query.AuthModeScoped, SubjectClass: "file_registry_token"},
		ok:   true,
	}

	chain := ChainResolvers(identity, bearer, file)
	auth, ok, err := chain.ResolveScopedToken(context.Background(), "some-credential")
	if err != nil {
		t.Fatalf("ResolveScopedToken() error = %v, want nil", err)
	}
	if !ok || auth.SubjectClass != "file_registry_token" {
		t.Fatalf("ResolveScopedToken() = %+v, %v, want the file registry's match", auth, ok)
	}
	if want := []string{"identity", "bearer", "file"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v (identity -> bearer -> file, in order)", calls, want)
	}
}

// TestChainResolversBearerMatchShortCircuitsBeforeFile proves a match at the
// bearer resolver (the middle of the chain) stops before ever consulting the
// file-registry resolver.
func TestChainResolversBearerMatchShortCircuitsBeforeFile(t *testing.T) {
	t.Parallel()

	var calls []string
	identity := &fakeChainResolver{name: "identity", calls: &calls}
	bearer := &fakeChainResolver{
		name: "bearer", calls: &calls,
		auth: query.AuthContext{Mode: query.AuthModeScoped, SubjectClass: "external_oidc_user"},
		ok:   true,
	}
	file := &fakeChainResolver{name: "file", calls: &calls}

	chain := ChainResolvers(identity, bearer, file)
	auth, ok, err := chain.ResolveScopedToken(context.Background(), "some-jwt-shaped-credential")
	if err != nil || !ok || auth.SubjectClass != "external_oidc_user" {
		t.Fatalf("ResolveScopedToken() = %+v, %v, %v, want the bearer resolver's match", auth, ok, err)
	}
	if file.called {
		t.Fatal("file-registry resolver was called after the bearer resolver already matched")
	}
	if want := []string{"identity", "bearer"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

// TestChainResolversBearerErrorFailsClosedBeforeFile proves a bearer-resolver
// DENIAL (a JWT-shaped credential that failed validation, returned as a
// non-nil error per oidcbearer's contract — see
// internal/oidcbearer/resolver.go's ResolveScopedToken doc comment) stops
// the chain immediately: it must never fall through to the file-registry
// resolver, which could not have understood the credential anyway, and the
// caller must see the denial, not an unrelated later resolver's opinion.
func TestChainResolversBearerErrorFailsClosedBeforeFile(t *testing.T) {
	t.Parallel()

	var calls []string
	denied := errors.New("oidcbearer: bearer token denied: expired")
	identity := &fakeChainResolver{name: "identity", calls: &calls}
	bearer := &fakeChainResolver{name: "bearer", calls: &calls, err: denied}
	file := &fakeChainResolver{name: "file", calls: &calls, ok: true}

	chain := ChainResolvers(identity, bearer, file)
	_, ok, err := chain.ResolveScopedToken(context.Background(), "expired-jwt")
	if !errors.Is(err, denied) {
		t.Fatalf("ResolveScopedToken() error = %v, want %v", err, denied)
	}
	if ok {
		t.Fatal("ResolveScopedToken() ok = true, want false on a denial error")
	}
	if file.called {
		t.Fatal("file-registry resolver was called after the bearer resolver denied with an error")
	}
	if want := []string{"identity", "bearer"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want %v", calls, want)
	}
}

// TestChainResolversNoMatchFallsThroughToSharedToken proves that when every
// chained resolver returns (false, nil) — an opaque credential no scoped
// resolver recognizes — the composite result is also (false, nil), letting
// authMiddlewareWithRoutePolicy fall back to the legacy shared-token check.
func TestChainResolversNoMatchFallsThroughToSharedToken(t *testing.T) {
	t.Parallel()

	var calls []string
	identity := &fakeChainResolver{name: "identity", calls: &calls}
	bearer := &fakeChainResolver{name: "bearer", calls: &calls}
	file := &fakeChainResolver{name: "file", calls: &calls}

	chain := ChainResolvers(identity, bearer, file)
	_, ok, err := chain.ResolveScopedToken(context.Background(), "unrecognized-credential")
	if err != nil || ok {
		t.Fatalf("ResolveScopedToken() = ok:%v err:%v, want (false, nil)", ok, err)
	}
	if want := []string{"identity", "bearer", "file"}; !equalStrings(calls, want) {
		t.Fatalf("call order = %v, want every resolver consulted in order: %v", calls, want)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
