// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package oidcbearer implements query.ScopedTokenResolver for IdP-issued
// OAuth2 access tokens presented as an Authorization: Bearer header on the
// Eshu API and MCP surfaces (issue #5162, epic #5161).
//
// Eshu's own scoped and shared tokens are opaque hashes; a caller presenting
// a compact JWT is always attempting IdP bearer authentication, never one of
// those. Resolver.ResolveScopedToken therefore gates on the credential's
// shape first: a non-JWT credential falls through to the rest of the
// resolver chain untouched, and a deployment with zero bearer IdPs enabled
// resolves instantly with no verifier built and no JWKS traffic (the
// zero-provider fast path). Once a credential is JWT-shaped and at least one
// provider is enabled, this package owns the verdict outright: it verifies
// signature, issuer, expiry, and audience (the canonical Eshu resource URI,
// RFC 8707, ESHU_AUTH_RESOURCE_URI) via go-oidc, maps the verified claims to
// grants through the SAME oidclogin.GrantResolver machinery interactive OIDC
// login uses, and either returns a scoped AuthContext or a distinct denied
// reason — it never falls through to a resolver that could not have
// understood a JWT anyway.
//
// The verifier cache (cache.go) is a lock-free-read, TTL-refreshed snapshot:
// requests always read an atomic pointer and never block on a rebuild: a
// stale snapshot triggers exactly one background rebuild (guarded so at most
// one runs at a time) while every concurrent request keeps using the
// current snapshot. A rebuild reuses a provider's existing verifier when its
// issuer and revision are unchanged, and only calls the (possibly
// network-bound) VerifierFactory for a provider that is new or changed.
// Because api and mcp-server are independent processes with no shared event
// bus, the TTL — not a push notification — is the only honest mechanism for
// provider CRUD (enable/disable/rotate) to become visible without a
// restart; see the README for the cross-process consistency argument.
package oidcbearer
