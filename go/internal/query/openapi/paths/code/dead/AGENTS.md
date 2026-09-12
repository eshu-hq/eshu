# Dead-Code OpenAPI Path Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/code/dead/` (package `dead`).

## Ownership

This leaf owns the OpenAPI path fragments for the dead-code routes:
`Investigation` (single-repository investigation), `Scan` (per-repository
scan), and `CrossRepo` (cross-repository classification). `openapi/spec.go`
concatenates all three individually — there is no `Routes` aggregate here,
unlike most `paths/<leaf>` packages, because the parent `code` package's own
`routes.go` already lists the code family's top-level routes and these three
are added alongside it, not folded into it.

- This package MUST NOT import `openapi` or the parent `code` package —
  either import would cycle, since `code` does not import this leaf either.
- Each file holds exactly one exported constant: `investigation.go` ->
  `Investigation`, `scan.go` -> `Scan`, `cross_repo.go` -> `CrossRepo`. Do
  not add a second constant to any of them.
- These are documentation fragments, not the routes themselves. A change to
  a real `code` dead-code handler's method, path, or request/response shape
  must be mirrored here or `scripts/verify-openapi.sh` will report drift.

## Verification

Keep `scripts/verify-openapi.sh` green after any change (see
`openapi/AGENTS.md` for what it checks), and update
`docs/public/reference/http-api.md` in the same PR for any wire-visible
change. Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/paths/code/dead` after any edit here.
