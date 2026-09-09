# Admin Identity — Agent Instructions

Scope: `go/internal/query/admin/identity/` (package `identity`).

## Ownership

The tenant identity admin surface: `ReadHandler` (invitations, role
assignments, roles, IdP providers, IdP group mappings, API tokens, audit
reads) and `MutationHandler` (invitation revokes, role assignment
grants/revokes, IdP group mapping writes), with their row models and store
ports.

## Rules

- Every route requires all-scope admin authentication and reads or writes
  strictly within the caller's own tenant. Tests must cover the
  wrong-tenant and no-tenant denials for any new route.
- Audit every allowed and denied mutation through the `audit` sibling
  package. Never log raw external group names or secrets.
- This package may import the parent `admin` package only for the shared
  actor identity, and the sibling `audit` package for the rest. It MUST
  NOT import the query root or `provider/config`.
