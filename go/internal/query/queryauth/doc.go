// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queryauth owns the request-scoped authorization bounds a query
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
// read the root symbols their census demanded, in five subjects: browser
// session cookies (CookieSecureMode and its validators, the cookie-name
// constants, the idle/absolute timeout defaults, BrowserSessionSecretHash,
// and WriteBrowserSessionCookies); the browser session wire types
// (BrowserSessionStore, BrowserSessionCreateRecord, BrowserSessionResponse,
// BrowserSessionAuthResponse, NormalizeBrowserSessionAuthContext, and
// BrowserSessionAuthResponseFor -- capitalizing root's unexported
// browserSessionAuthResponse to plain BrowserSessionAuthResponse would have
// collided with the type of that name, so it kept the "For" suffix); session
// timeout resolution (ResolveSessionTimeouts); and the read-only sign-in
// policy shape (SignInPolicy, SignInPolicyReadStore). GovernanceAuditAppender
// and ActorClassForAuth moved too, so a handler-family subpackage can accept
// an audit appender and classify an AuthContext for its own audit rows.
// unauthorizedResponse and writePermissionDeniedEnvelope did NOT move here:
// they need querycontract's WriteJSON/ResponseEnvelope/ErrorEnvelope
// primitives, and this package cannot import querycontract (querycontract
// already imports this package for RepositoryAccessFilterFromContext, and
// the reverse edge would cycle) -- see querycontract.WriteUnauthorized and
// querycontract.WritePermissionDenied instead.
package queryauth
