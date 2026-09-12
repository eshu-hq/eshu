# Query authorization context

## Purpose

The request-scoped authorization bounds a query handler enforces: which mode
authenticated the caller, which tenant and workspace they belong to, and which
scope and repository ids they may read. Plus the context slot those bounds
travel in, the permission-catalog predicates a handler asks before serving,
and (#6642) the browser-session cookie/type shapes, session-timeout
resolution, read-only sign-in policy, and audit-actor classification the
local-identity and setup family moves need.

## Ownership boundary

This package owns the `AuthContext` shape, its context key, the
permission-catalog feature and data-class predicates, and the five #6642
subjects below. It does not authenticate anyone. Middleware, token
resolution, session handling, and CSRF enforcement stay in the root query
package; this holds only what a handler needs to read once a request is
already authenticated, plus the wire shapes those handlers exchange.

| Subject | File here | What root keeps |
| --- | --- | --- |
| Browser session cookies | `session_cookies.go` | exported const/type aliases and function forwarders |
| Browser session wire types | `browser_session_types.go` | exported type aliases and function forwarders |
| Session timeout resolution | `session_timeouts.go` | function forwarder |
| Read-only sign-in policy | `sign_in_policy.go` | exported type aliases; root keeps the write-side `SignInPolicyUpdateRequest`/`SignInPolicyMutationStore`/sentinel errors, which no hoisted symbol needs |
| Audit actor classification | `audit_actor.go` | exported type alias and function forwarder |

`unauthorizedResponse` and `writePermissionDeniedEnvelope` did NOT move
here -- see [doc.go](doc.go) for why, and `querycontract.WriteUnauthorized` /
`querycontract.WritePermissionDenied` for where they live instead.

## Exported surface

`AuthContext`, `AuthMode` and its three constants, `AuthContextFromContext`,
`ContextWithAuthContext`, and `CleanedStrings`. The permission-catalog
surface: `AllowsPermissionFeature`, `AllowsPermissionDataClasses`,
`PermissionFeatureAskSearch`, and `PermissionDataClassesAskSearch`. The
#6642 surface: `CookieSecureMode` and its constants/validators,
`BrowserSessionCookieName` and its siblings, `DefaultBrowserSessionIdleTimeout`,
`DefaultBrowserSessionAbsoluteTimeout`, `BrowserSessionSecretHash`,
`WriteBrowserSessionCookies`, `BrowserSessionStore`,
`BrowserSessionCreateRecord`, `BrowserSessionResponse`,
`BrowserSessionAuthResponse`, `NormalizeBrowserSessionAuthContext`,
`BrowserSessionAuthResponseFor`, `ResolveSessionTimeouts`, `SignInPolicy`,
`SignInPolicyReadStore`, `GovernanceAuditAppender`, and `ActorClassForAuth`.
See [doc.go](doc.go).

## Dependencies

The Go standard library, plus `internal/governanceaudit` for the
`governanceaudit.Event`/`governanceaudit.ActorClass` types
`GovernanceAuditAppender` and `ActorClassForAuth` carry. Neither brings a
transitive dependency on the root query package or `querycontract`.

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

## Performance and observability of the session seam (#6642)

No-Regression Evidence (#6642 seam, baseline 11c6ab9b8): the hot file this change touches is
root's `auth_audit.go`, where `actorClassForAuth` became a one-line forwarder
to `ActorClassForAuth` in `audit_actor.go`; the function body moved
verbatim, so the audit actor-class decision runs the same branches on the
same `AuthContext` fields. Baseline `origin/main` vs this branch:
`go build ./...`, `go vet ./internal/query/...`, `go test
./internal/query/... -count=1` and the root, `queryauth` and `querycontract`
test-name union are unchanged (nothing dropped, added, or duplicated); no
Cypher, SQL, queue, lease, or worker path is involved, so there is no
throughput or latency surface to measure beyond one extra call frame per
audit record, which the compiler inlines.

No-Observability-Change (#6642 seam): no metric, span, log, or status
surface is added, removed, or renamed. `ActorClassForAuth` still returns
the same `governanceaudit.ActorClass` values for the same inputs, so audit
records keep their actor-class labels, and the 401 and 403 envelopes written
by `querycontract.WriteUnauthorized` and `WritePermissionDenied` keep their
exact status codes, error codes and `WWW-Authenticate` values, and derive
their correlation identifiers the same way (request header first, then a
fresh random hex value).

## Related docs

- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
