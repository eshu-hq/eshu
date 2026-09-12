# CI/CD OpenAPI Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/cicd/` (package `cicd`).

## Ownership

This leaf owns the OpenAPI 3.0 path documentation for the CI/CD
run-correlation surface: `routes.go` (`Routes`) and
`run_correlation_aggregate.go` (`RunCorrelationAggregate`). Each file is a
JSON string constant only — no handler logic, no HTTP wiring.

- This package imports nothing from the rest of the repo. It MUST NOT
  import the parent `openapi` package or the `query` root — both import
  this package, so either direction is a cycle.
- A constant here is inert until `openapi/spec.go` concatenates it into the
  published spec. Adding, renaming, or removing an exported constant here
  without updating `spec.go` in the same change silently changes the
  published OpenAPI document without changing what the server actually
  serves, or vice versa.

## Naming

`docs/internal/naming.md` is law: no `cicd_` file prefixes, no
`cicd/cicd.go`, and exported constants lose the family stutter (`Routes`,
not CicdRoutes).

## Verification

A change here MUST keep `scripts/verify-openapi.sh` green — it cross-references
`mux.HandleFunc` registrations against these fragments and fails on either an
undocumented route or a documented route with no handler. A route path or
method change here MUST update
[HTTP API Reference](../../../../../../docs/public/reference/http-api.md) in
the same PR, per the root `CLAUDE.md` documentation rule.
