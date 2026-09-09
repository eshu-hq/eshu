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
- This package may import the parent `admin` package only for the shared
  actor identity, and the sibling `audit` package for the rest. It MUST
  NOT import the query root.
