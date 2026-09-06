# #6459: browser sessions get their own governance-audit actor class

Follow-up to the all-scope browser-session admission record
(`6450-all-scope-browser-session-admission.md`, residual item 6). Before this
change a cookie session refused on a grant-bound route was audited as
`scoped_token`, so an operator filtering denied reads could not tell a
console session from a bearer.

## What changed

`ActorClassBrowserSession` (`browser_session`) joins the
`governanceaudit.ActorClass` vocabulary and `validActorClass`, and
`recordScopedRouteAuthorizationDeniedWithReason` in
`go/internal/query/auth_audit.go` picks it when `AuthContext.Mode` is
`AuthModeBrowserSession`. Bearer denials keep `scoped_token`; a blank subject
hash still downgrades to `anonymous`. Local-identity writes and admin
recovery keep their own mappings; #6566 tracks aligning them.

## Evidence

Observability Evidence: the audit row for a cookie-session denial now carries
`actor_class = browser_session`; the runbook in
`docs/public/operate/hosted-governance.md` names it. Pinned by
`TestNormalizeEventAcceptsBrowserSessionActor` (`go/internal/governanceaudit`),
`TestAuthMiddlewareAllScopesBrowserSessionRefusedOnGrantBoundRouteUnderFailClosedPolicy`
and `TestRecordScopedRouteAuthorizationDeniedBlankReasonFallsBackToUnspecified`
(`go/internal/query`), with a bearer-mode case proving `scoped_token` is
unchanged. Operators filtering by actor class see the new value from this
change on.

No-Regression Evidence: one enum member and one branch on a field the helper
already holds; no query, statement, lease or worker path changes. The
`actor_class` column is `TEXT` with no constraint (migration 006b), so no
migration runs. `go test ./internal/query ./internal/governanceaudit
./internal/governanceauditasync -count=1` passes at the branch head.
