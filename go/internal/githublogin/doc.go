// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package githublogin handles backend GitHub Authorization Code login for
// dashboard browser sessions (issue #5166, F-5 of epic #5161).
//
// github.com user login is plain OAuth2, not OpenID Connect: there is no
// `/.well-known/openid-configuration` discovery document for github.com, no
// ID token, and no JWKS. Identity comes entirely from calling the GitHub
// REST API (/user, /user/emails, /user/memberships/orgs, /user/teams) with
// the exchanged access token. The existing discovery-based
// internal/oidclogin connector (built on coreos/go-oidc) cannot serve this
// provider at all, so this package is a dedicated, non-discovery OAuth2
// connector that normalizes GitHub identity into the SAME
// query.AuthContext + browser-session shape oidclogin produces.
//
// This package reuses oidclogin's GrantResolver/GrantQuery/GrantResolution/
// StaticGrantResolver/GroupRoleMapping/RoleGrant types unchanged (aliased in
// service_types.go) rather than redefining a parallel group→role mapping
// concept: a GitHub team handle ("org/team-slug") is hashed with the same
// SHA256Hash function an OIDC group claim value is, and fed into the
// identical GrantQuery.GroupHashes field. The DB-backed
// identity_provider_group_role_mappings table has no provider_kind column —
// it is keyed only on provider_config_id + external_group_hash — so the
// same mapping rows and the same resolver code path serve OIDC groups and
// GitHub teams equivalently, which is how issue #5166's "team→role mapping
// proven equivalent to OIDC group→role for the same grants fixture"
// acceptance criterion is satisfied structurally rather than by parallel
// re-implementation.
//
// Unlike an OIDC provider (which trusts the IdP's own tenant boundary), a
// GitHub OAuth App can authenticate any github.com (or GitHub Enterprise
// Server) account. AllowedOrgs is therefore mandatory and non-empty on every
// ProviderConfig — it is this connector's only tenant boundary. A user with
// no active membership in an allowed org is denied with an audited reason
// (see service.go's CompleteGitHubLogin) before any session is issued.
//
// A GitHub access token is opaque: it cannot be validated the way an OIDC
// ID token or a JWKS-verified bearer token can (there is no issuer, no
// audience, no JWKS to check against), and github.com is not an MCP-spec
// (RFC 9728) authorization server. GitHub-only orgs therefore continue to
// use per-user issued API tokens for MCP access (issue #5164, F-3) rather
// than pretending a GitHub-backed browser session implies MCP bearer-token
// validation — see this package's README for the recorded decision and
// docs/internal/design/3452-user-management-identity-federation.md for the
// broader identity-federation design this connector extends.
package githublogin
