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
- A failed store, tester, or generator step answers through
  `tracing.WriteServerFailure` with a fixed message constant from
  `failure.go`, never `err.Error()` or a string built from `err` (#7674): 500
  with the error on the request span, or 499 on a client cancel. Keep the
  route's own 4xx and configuration 503 answers before it.
  `server_failure_test.go` drives every step through
  `testutil/failure`; add a route there for any new failing step.
- The two audit reads (`audit.go`) go through the guarded PostgreSQL reader:
  each failure site calls `querycontract.WriteGraphReadError` with its
  capability-matrix key first (`audit.events.list`, `audit.summary.read`,
  #8227), then its fixed message, and logs only a server fault, not a client cancel.
  `audit_failure_test.go` covers them.
- This package may import the parent `admin` package only for the shared
  actor identity, and the sibling `audit` package for the rest. It MUST
  NOT import the query root or `provider/config`.
