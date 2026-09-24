// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package local implements the production local-identity handler family
// (#6642, split off the #6060 lane A query-root restructure): IdentityHandler,
// its Mount method, and every local-identity route it registers --
// bootstrap, login, invitations, password reset/rotation, MFA reset, user
// disablement, break-glass recovery, generated API-token create/list/revoke/
// rotate, and self-service TOTP enrollment.
//
// Every mutating route that needs an authenticated admin caller checks
// AllScopes through auth.AuthContextFromContext before touching the
// store, and every route that gates on a permission-catalog feature calls
// auth.AllowsPermissionFeature (through this package's own
// requirePermissionFeature, which also emits the governance-audit denial)
// before proceeding. Every route reads and writes only through
// IdentityProfileLister (this package's storage port); no handler in this
// package touches a graph or Postgres driver directly. Login, password
// rotation, and break-glass session issuance all funnel through
// IssueSessionCookies, the one place a server-managed browser session is
// created and its cookies written.
//
// This package imports auth (AuthContext, browser-session types,
// sign-in-policy read port, the shared handler-tracing-free HTTP auth
// primitives) and querycontract (envelope writers, ReadJSON/WriteJSON/
// WriteError, PathParam, WriteUnauthorized, WritePermissionDenied,
// RequirePermissionFeature); it MUST NOT import the query root, or root
// would cycle back through its own compatibility aliases in
// local_identity_alias.go, which import this package for the type aliases
// and forwarders root and the setup family's staying files still use. See
// README.md for the file layout and move evidence, and AGENTS.md for the
// per-symbol export rationale.
package local
