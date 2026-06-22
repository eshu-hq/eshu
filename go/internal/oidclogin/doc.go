// Package oidclogin implements the backend OIDC Authorization Code login flow
// for dashboard browser sessions.
//
// The package stores only hashes for state, nonce, redirect, subject, and group
// claim values. Provider tokens and raw claims stay transient while the service
// verifies issuer metadata, JWKS-backed ID tokens, audience, expiry, nonce, and
// group-to-role mappings before returning an Eshu query AuthContext plus
// hash-only provider proof metadata. Browser-session staleness enforcement is
// intentionally outside this login package.
package oidclogin
