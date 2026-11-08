// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package auth owns the request-scoped authorization bounds a query
// handler reads, and the context slot they travel in.
//
// AuthContext carries the caller's mode, tenant, and allow-lists.
// ContextWithAuthContext stores it; AuthContextFromContext reads it back. The
// key those two use is unexported and defined only here, so every writer and
// reader in the query surface addresses the same slot.
//
// AllowsPermissionFeature and AllowsPermissionDataClasses answer the
// permission-catalog questions a handler asks before serving. Both fail open on
// the three pre-catalog cases — no auth context, the catalog not enforced, and
// the legacy shared bearer path — because the catalog gates callers carrying a
// derived grant snapshot, and failing closed there would deny every deployment
// that has not enabled it. AllowsPermissionDataClasses requires every requested
// class, not any.
//
// It is a leaf so a handler-family subpackage can read the auth context without
// importing the root query package, which it cannot do without an import cycle
// (#6060). AuthContext and AuthMode carry no methods, so package query aliases
// both and existing callers, including those outside internal/query, are
// unaffected.
//
// #6642 extended the seam so the local-identity and setup family moves can
// read the root symbols their census demanded, in five subjects. #6818 move
// 4a relocated three of those subjects to package session
// (internal/query/auth/session): browser session cookies (CookieSecureMode
// and its validators, the cookie-name constants, the idle/absolute timeout
// defaults, BrowserSessionSecretHash, and WriteBrowserSessionCookies); the
// browser session wire types (BrowserSessionStore, BrowserSessionCreateRecord,
// BrowserSessionResponse, BrowserSessionAuthResponse,
// NormalizeBrowserSessionAuthContext, and BrowserSessionAuthResponseFor --
// capitalizing root's unexported browserSessionAuthResponse to plain
// BrowserSessionAuthResponse would have collided with the type of that name,
// so it kept the "For" suffix); and session timeout resolution
// (ResolveSessionTimeouts) -- leaving the read-only sign-in policy shape
// (SignInPolicy, SignInPolicyReadStore), GovernanceAuditAppender, and
// ActorClassForAuth here, so a handler-family subpackage can accept an audit
// appender and classify an AuthContext for its own audit rows without
// importing root.
// unauthorizedResponse and writePermissionDeniedEnvelope did NOT move here:
// they need querycontract's WriteJSON/ResponseEnvelope/ErrorEnvelope
// primitives, and this package cannot import querycontract (querycontract
// already imports this package for RepositoryAccessFilterFromContext, and
// the reverse edge would cycle) -- see querycontract.WriteUnauthorized and
// querycontract.WritePermissionDenied instead.
//
// D13 (eshu-hq/eshu#9669) moved the external-provider login contracts here
// (login_contracts.go: the GitHub/OIDC admission ports, the SSO denial
// wrapper and reason extractor, the bearer-unrecognized sentinel, and the
// resolver interfaces) and the MFA recovery-code issuance contract
// (mfa_recovery.go). Root keeps aliases and one forwarder so in-tree router
// callers compile unchanged; the HTTP handlers, error-to-status writers,
// and audit emitters stay in root until the removal PR relocates them.
//
// D13 row-2 leaf 2b moved the Group C auth ledgers here as well: the
// shared-key-only and pending-row-filtering ledgers, the scoped-token
// allowlist decision and its MCP surfaces, the browser-session/all-scope
// route policy and its denial reasons, and the route-denial signal
// (scoped_routes*.go, browser_session_route_policy.go,
// route_denial_signal.go). Root keeps same-named forwarders (plus the
// BrowserSessionRoutePolicy alias) so the middleware, the root tests, and
// the MCP exhaustiveness gate read the same values with no duplicated
// ledger; the audit emitters and middleware constructors stay in root with
// the rest of the handler wiring.
package auth
