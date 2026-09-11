# Auth OpenAPI Path Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/auth/` (package `auth`).

## Ownership

This leaf owns the OpenAPI path fragments for `/api/v0/auth/...`: `Routes`
(provider discovery and sign-in posture), `Setup`, `Tokens`, `TOTP`,
`AdminReads`, `AdminMutations`, `AdminProviderConfigs`, and
`SignInPolicy`, one exported constant per file. `openapi/spec.go`
concatenates all eight; keep that call site's order in mind when reasoning
about where in the published document a change here lands.

- This package MUST NOT import `openapi` — the parent imports this
  package, and the reverse would cycle.
- These are documentation fragments, not the routes themselves. The actual
  `auth` handlers and their `mux.HandleFunc` registrations live elsewhere
  in `go/internal/query/`; a change to a real route's method, path, or
  request/response shape must be mirrored here or `scripts/verify-openapi.sh`
  will report drift.

## The `routes.go` file-length exemption

`routes.go` is 798 lines and carries `//nolint:filelength` with an inline
justification. That exemption exists because this file is a single Go
string-literal constant documenting one route family's request and
response shapes in full; splitting the string across files would break the
per-fragment review boundary `internal/query/AGENTS.md` establishes for
these files specifically (each `openapi_paths_*`-lineage file is meant to
be reviewed as one self-contained fragment).

Do not copy this exemption onto a new file just because it is getting
long. It is justified here because the fragment is already one coherent
unit (the whole `/api/v0/auth/...` provider/admin surface) that splitting
would make harder, not easier, to review. A new file that is long because
it bundles unrelated routes should be split into separate route-scoped
files instead — that is the normal case every other file in this package
already follows.

## Verification

Keep `scripts/verify-openapi.sh` green after any change (see
`openapi/AGENTS.md` for what it checks), and update
`docs/public/reference/http-api.md` in the same PR for any wire-visible
change. Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/paths/auth` after any edit here.
