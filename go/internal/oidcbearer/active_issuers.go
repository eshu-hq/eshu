// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package oidcbearer

import (
	"context"
	"sort"
)

// ActiveIssuers returns the sorted, deduplicated issuer URLs currently
// enabled for bearer-token validation: exactly the set ResolveScopedToken
// would route a token to via snap.byIssuer. Issue #5163 (F-2)'s RFC 9728
// protected-resource metadata document consumes this for its
// "authorization_servers" field through the query.OAuthAuthorizationServerLister
// interface, which this method implements structurally, so the metadata
// document never names an issuer this deployment could not actually validate
// a token against.
//
// Two active providers that share one issuer are fail-closed excluded from
// the routing table (see cache.go's rebuild doc comment: the token would be
// denied as unknown-issuer rather than mis-routed to the wrong tenant), so
// ActiveIssuers excludes that issuer too — advertising it as a working
// authorization server would be dishonest.
//
// A nil *Resolver (the shape callers hold when ESHU_AUTH_RESOURCE_URI is
// unset — see cmd/api and cmd/mcp-server's oidc_bearer_wiring.go) returns
// nil rather than panicking. This never triggers a synchronous rebuild: it
// reads whatever snapshot currentAndMaybeRebuild currently has cached
// (possibly momentarily stale by up to the configured TTL) and lets that
// call's own background-refresh trigger handle staleness, exactly like
// ResolveScopedToken does.
func (r *Resolver) ActiveIssuers(ctx context.Context) []string {
	if r == nil {
		return nil
	}
	snap := r.cache.currentAndMaybeRebuild(ctx)
	if snap.empty() {
		return nil
	}
	issuers := make([]string, 0, len(snap.byIssuer))
	for issuer := range snap.byIssuer {
		issuers = append(issuers, issuer)
	}
	sort.Strings(issuers)
	return issuers
}
