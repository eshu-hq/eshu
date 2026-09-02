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
package queryauth
