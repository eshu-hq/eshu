# AGENTS — internal/scopedtoken

Scoped per-team bearer-token resolution for the hosted API/MCP read surface.
Read `README.md` and `doc.go` before editing.

## Invariants (do not regress)

- Store only `token_sha256`; never persist, log, or return a plaintext token.
- Errors must never include token-hash material, credentials, file contents, or
  grant ids. The duplicate-hash and parse paths have tests asserting this.
- `LoadRegistryFromFile` fails closed: any malformed entry, duplicate hash,
  missing tenant/workspace, or unsupported version is a hard error. Never load a
  partial grant table.
- `ResolveScopedToken` returns `(zero, false, nil)` for empty/unknown
  credentials so the middleware can fall through to shared-token or
  unauthenticated handling. Do not return an error for "not found".
- The returned `AuthContext` must be a defensive copy (fresh grant slices) so a
  handler cannot mutate the shared registry.
- `Mode` must be `query.AuthModeScoped` on every resolved context.

## Boundaries

- This package produces `query.AuthContext`; it does not enforce routes. The
  scoped-route allowlist and bounded query filters live in `internal/query`
  (`auth_scoped_routes.go`, the per-family handlers). Enforcement is only as
  strong as those filters — adding a new route to the gate requires the
  matching bounded filter, not a change here.
- Issuance/rotation is operator-managed via the secret-mounted registry file
  (`ESHU_SCOPED_TOKENS_FILE`). This package only reads it.

## Tests

`registry_test.go` covers resolve, unknown/empty credential, all-scopes admin,
every load-validation failure, and the no-leak error contract. Add a case for
any new field or validation rule.
