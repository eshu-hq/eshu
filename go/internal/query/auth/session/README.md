# session

Browser-session family behind the query handler surface (Issue #6818,
move 4a): browser-session wire types, cookie issuance and clearing,
idle/absolute timeout resolution, and browser-session auth-context
normalizers.

## Ownership

- `queryauth` owns `AuthContext`, `AuthMode`, and the context key. This
  package names `queryauth.AuthContext`/`queryauth.AuthMode`; `queryauth`
  does not import this package.
- Root package `query` keeps compatibility aliases (`BrowserSessionStore`,
  `BrowserSessionCreateRecord`, timeout defaults) and thin forwarders so
  existing call sites keep compiling; new code spells `session.` names.
- Cookie helpers in this package must not gain handler or route
  responsibilities; orchestration stays with the handlers.

## Verification

From `go/`: `go test ./internal/query/auth/session/ ./internal/query/queryauth/ -count=1`.
The cookie `__Host-` vs bare name pairing is covered by
`cookies_test.go` (#4964); the timeout matrix by the timeout tests
(#4968).
