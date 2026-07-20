// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"testing"
)

// TestActiveIssuers_NoProviders proves the zero-provider snapshot (AC #4's
// fast path — no verifier factory call, no JWKS traffic) reports zero active
// issuers rather than a nil-vs-empty ambiguity, matching snapshot.empty()'s
// own definition of "nothing enabled".
func TestActiveIssuers_NoProviders(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	resolver, _ := newTestResolver(t, idp, nil, testGrantResolver(), nil)

	issuers := resolver.ActiveIssuers(context.Background())
	if len(issuers) != 0 {
		t.Fatalf("ActiveIssuers() = %v, want empty for zero providers", issuers)
	}
}

// TestActiveIssuers_ReturnsSortedUniqueIssuers proves ActiveIssuers reflects
// exactly the issuer set ResolveScopedToken would route a token to — the
// source issue #5163 (F-2)'s RFC 9728 authorization_servers field consumes so
// it never advertises an issuer this deployment cannot actually validate.
func TestActiveIssuers_ReturnsSortedUniqueIssuers(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	providers := []BearerProvider{
		{ProviderConfigID: "pc_b", IssuerURL: "https://b.example.test", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevisionID: "rev_1"},
		{ProviderConfigID: "pc_a", IssuerURL: "https://a.example.test", TenantID: "tenant_a", WorkspaceID: "workspace_a", RevisionID: "rev_1"},
	}
	resolver, _ := newTestResolver(t, idp, providers, testGrantResolver(), nil)

	issuers := resolver.ActiveIssuers(context.Background())
	want := []string{"https://a.example.test", "https://b.example.test"}
	if len(issuers) != len(want) || issuers[0] != want[0] || issuers[1] != want[1] {
		t.Fatalf("ActiveIssuers() = %v, want sorted %v", issuers, want)
	}
}

// TestActiveIssuers_ExcludesAmbiguousSharedIssuer proves ActiveIssuers never
// names an issuer two active providers share: cache.rebuild fails closed on
// that ambiguity (a token for it is denied as unknown-issuer rather than
// mis-routed to the wrong tenant — see cache.go's rebuild doc comment), so
// the metadata document must not advertise it as a working authorization
// server either.
func TestActiveIssuers_ExcludesAmbiguousSharedIssuer(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	providers := []BearerProvider{
		{ProviderConfigID: "pc_1", IssuerURL: testIssuer, TenantID: "tenant_a", WorkspaceID: "workspace_a", RevisionID: "rev_1"},
		{ProviderConfigID: "pc_2", IssuerURL: testIssuer, TenantID: "tenant_b", WorkspaceID: "workspace_b", RevisionID: "rev_1"},
	}
	resolver, _ := newTestResolver(t, idp, providers, testGrantResolver(), nil)

	issuers := resolver.ActiveIssuers(context.Background())
	if len(issuers) != 0 {
		t.Fatalf("ActiveIssuers() = %v, want empty when two providers share one issuer (fail closed)", issuers)
	}
}

// TestActiveIssuers_NilResolver proves a nil *Resolver (the shape
// newOIDCBearerResolver returns when ESHU_AUTH_RESOURCE_URI is unset, wrapped
// in a query.ScopedTokenResolver interface — see cmd/api and cmd/mcp-server's
// oidc_bearer_wiring.go) does not panic when a caller asks for its active
// issuers before checking for nil.
func TestActiveIssuers_NilResolver(t *testing.T) {
	t.Parallel()
	var resolver *Resolver
	if issuers := resolver.ActiveIssuers(context.Background()); issuers != nil {
		t.Fatalf("ActiveIssuers() on nil resolver = %v, want nil", issuers)
	}
}
