// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package oidclogin implements the backend OIDC Authorization Code login flow
// for dashboard browser sessions.
//
// The package stores only hashes for state, nonce, redirect, subject, and group
// claim values. Provider tokens and raw claims stay transient while the service
// verifies issuer metadata, JWKS-backed ID tokens, audience, expiry, nonce, and
// group-to-role mappings before returning an Eshu query AuthContext plus
// hash-only provider proof metadata. Browser-session staleness enforcement is
// intentionally outside this login package.
//
// The package also provides Refresher, a bounded active-session revocation
// refresh engine. Each pass reads a bounded batch of OIDC-backed sessions whose
// provider proof has reached its staleness window and, per session, either
// extends the bounded proof window after re-confirming the Eshu-owned
// authorization snapshot or revokes the session. Refresher re-resolves only
// hash-only identity and Eshu-owned role grants; it never re-queries the
// provider directly, since no raw provider token is persisted. External group
// removal is enforced by forced reauthentication at the window boundary.
package oidclogin
