# Agent instructions: session

This is an authorization surface. Read `doc.go` and `README.md` before
editing, and treat every change here as a security change.

## Invariants

- `auth` owns the context key, `AuthContext`, and `AuthMode`. Do not
  redefine any of them here; a second key type compiles cleanly and makes
  scoped requests read as unauthenticated.
- Keep the `__Host-` vs bare cookie-name pairing in `cookies.go` exactly
  as documented: a `__Host-`-prefixed cookie sent with `Secure=false` is
  invalid per RFC 6265bis and browsers reject it outright (#4964).
- `ResolveSessionTimeouts` must keep preferring per-tenant sign-in policy
  values and falling back to the package defaults (#4968).
- This package MUST NOT import `querycontract`. `querycontract` reads
  `auth.AuthContext`, and the session import of `auth` would
  close a cycle through the reverse edge.

## Verification

From `go/`:
`go test ./internal/query/auth/session/ ./internal/query/auth/ -count=1`.
Confirm the session suite ran a real case count rather than matching zero.
