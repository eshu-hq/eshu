// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"errors"
	"testing"
)

// TestGrantStoreOutageIsUnavailableNotNoGrants proves that an infrastructure
// failure reaching the grant store is reported as an availability problem, not
// as the subject having no grants.
//
// The distinction is not cosmetic. `no_grants` is a statement about a person —
// "this identity is not entitled to anything here" — and during a grant-store
// outage every authenticating user would have that recorded against them in the
// governance audit trail and counted under that outcome in the resolver metric.
// An operator reading either during an incident would see a mass entitlement
// failure rather than a database being down.
func TestGrantStoreOutageIsUnavailableNotNoGrants(t *testing.T) {
	t.Parallel()

	idp := newTestIdP(t)
	grants := testGrantResolver()
	grants.err = errors.New("grant store unavailable")
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, grants, nil)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok {
		t.Fatal("ResolveScopedToken() ok = true, want false on a grant-store outage")
	}
	if err == nil {
		t.Fatal("ResolveScopedToken() err = nil, want a denial error")
	}

	var carrier interface{ DenialOutcome() string }
	if !errors.As(err, &carrier) {
		t.Fatalf("denial error does not expose DenialOutcome: %v", err)
	}
	if got := carrier.DenialOutcome(); got != outcomeGrantResolutionUnavailable {
		t.Fatalf("DenialOutcome() = %q, want %q", got, outcomeGrantResolutionUnavailable)
	}
}

// TestGenuinelyNoGrantsStaysNoGrants proves the outage classification did not
// swallow the real no-grants case: a grant store that answers successfully with
// no matching grants still reports no_grants.
func TestGenuinelyNoGrantsStaysNoGrants(t *testing.T) {
	t.Parallel()

	idp := newTestIdP(t)
	grants := &fakeGrantResolver{grantedForGroupHash: "no-such-hash-will-ever-match"}
	resolver, _ := newTestResolver(t, idp, []BearerProvider{testProvider()}, grants, nil)

	token := idp.sign(t, defaultTokenClaims(testIssuer, testAudience), false)
	_, ok, err := resolver.ResolveScopedToken(context.Background(), token)
	if ok {
		t.Fatal("ResolveScopedToken() ok = true, want false when no grants match")
	}
	var carrier interface{ DenialOutcome() string }
	if !errors.As(err, &carrier) {
		t.Fatalf("denial error does not expose DenialOutcome: %v", err)
	}
	if got := carrier.DenialOutcome(); got != outcomeNoGrants {
		t.Fatalf("DenialOutcome() = %q, want %q", got, outcomeNoGrants)
	}
}
