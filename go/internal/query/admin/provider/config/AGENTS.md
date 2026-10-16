# Admin Provider Config — Agent Instructions

Scope: `go/internal/query/admin/provider/config/` (package `config`).

## Ownership

The identity provider-config admin surface: `ReadHandler` (list, get,
revisions) and `MutationHandler` (create, update, revert, enable, disable,
test connection) for external identity providers, with the detail models,
write builders, login-readiness check, and store ports.

## Rules

- Every route requires all-scope admin authentication. Tests must cover
  the wrong-tenant and no-tenant denials for any new route.
- Secrets never leave the store boundary: responses and audit events carry
  presence flags, never values. The leakage tests pin this; extend them
  with any new secret-bearing field.
- Environment-managed providers are read-only (`ErrManagedByEnvironment`).
  Keep the kind lockstep green: a new provider kind needs its builder
  case, its spec enums, and the shared list entry together.
- A failed store, tester, or generator step answers through
  `tracing.WriteServerFailure` with a fixed message constant from
  `failure.go`, never `err.Error()` or a string built from `err` (#7674): 500
  with the error on the request span, or 499 on a client cancel. Keep the
  route's own 4xx and configuration 503 answers before it.
  `server_failure_test.go` drives every step through
  `testutil/failure`; add a route there for any new failing step.
- `writeProviderConfigWriteError` maps a classified store error to its 4xx
  or 503 and sends every other error to `tracing.WriteServerFailure`.
- This package may import the parent `admin` package only for the shared
  actor identity, and the sibling `audit` package for the rest. It MUST
  NOT import the query root.
