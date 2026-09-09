# Admin Audit Glue — Agent Instructions

Scope: `go/internal/query/admin/audit/` (package `audit`).

## Ownership

One home for the audit/permission glue the admin packages share: the
`Appender` port, the auth-to-actor mapping, correlation helpers, the
permission-feature gate, the identity hashes, and the optional-time row
shaping. Each helper cites the query-root source it was repointed from.

## Rules

- The logic sources stay canonical in the root (auth, permission catalog,
  local identity, documentation) and in the `queryauth` / `querycontract`
  leaves. This package only re-sources them so admin code never imports the
  query root.
- Do not add handler routes or store code here. Do not import `admin`,
  `identity`, `config`, or `store` (cycle).
- `SharedActorIDHash` is a singleton value: every surface stamps the same
  synthetic identity. Never duplicate its initializer.
