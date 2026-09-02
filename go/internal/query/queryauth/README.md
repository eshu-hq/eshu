# Query authorization context

## Purpose

The request-scoped authorization bounds a query handler enforces: which mode
authenticated the caller, which tenant and workspace they belong to, and which
scope and repository ids they may read. Plus the context slot those bounds
travel in, and the permission-catalog predicates a handler asks before serving.

## Ownership boundary

This package owns the `AuthContext` shape, its context key, and the
permission-catalog feature and data-class predicates. It does not authenticate
anyone. Middleware, token resolution, session handling, and CSRF
enforcement stay in the root query package; this holds only what a handler needs
to read once a request is already authenticated.

## Exported surface

`AuthContext`, `AuthMode` and its three constants, `AuthContextFromContext`,
`ContextWithAuthContext`, and `CleanedStrings`. Plus the permission-catalog
surface: `AllowsPermissionFeature`, `AllowsPermissionDataClasses`,
`PermissionFeatureAskSearch`, and `PermissionDataClassesAskSearch`. See
[doc.go](doc.go).

## Dependencies

The Go standard library only: `context` and `strings`.

## Telemetry

No-Observability-Change: this package emits no metric, span, or log. It stores
and returns a value. Authentication decisions and their audit events stay in the
root package's middleware, which is unchanged.

## Gotchas / invariants

**The context key is defined here and only here.** That is the single most
important property of this package. If package `query` kept a key type of its
own, middleware would write under one key while a handler family read under
another. The code compiles, it type checks, and every scoped request reads as
unauthenticated at runtime.

That is not theoretical. Reintroducing a second key in root and rerunning the
suite: the tree still **builds at exit 0**, and **316 tests fail** across the
scoped-grant and admin surfaces. Restoring the single key returns the suite to
exit 0. Any future change that adds a key type, here or in a caller, must be
checked the same way.

`AuthContext` and `AuthMode` deliberately have no methods, which is what lets
root alias them so `internal/oidcbearer`, `internal/scopedtoken`, and
`internal/ask/engine` keep naming `query.AuthContext` unchanged. Adding an
unexported method to either would break those callers, because a type alias
cannot reach unexported methods across a package boundary.

**The permission predicates fail open on three pre-catalog cases**: no auth
context, `PermissionCatalogEnforced` false, and `AuthModeShared`. That is
deliberate — the catalog gates callers carrying a derived grant snapshot, and
failing closed would deny every deployment that has not enabled it. Any change
that narrows those exits denies live traffic; any change that widens them grants
it. `AllowsPermissionDataClasses` requires **every** requested class, not any.

`PermissionFeatureAskSearch` and `PermissionDataClassesAskSearch` live here
rather than beside a handler because the two handlers that authorize against
them no longer share a package: the semantic-search family moved to
`internal/query/semanticsearch` for #6060 while the ask handler stayed in root.
Two copies of the strings would authorize against two different names, and a
caller granted one would be denied by the other.

`CleanedStrings` preserves order while trimming, dropping empties, and
de-duplicating. Allow-list comparisons depend on it, so a change to its
semantics is an authorization change.

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
