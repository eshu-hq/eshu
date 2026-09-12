# OpenAPI Assembly — Agent Instructions

Scope: `go/internal/query/openapi/` (package `openapi`, top-level files
only; `schema/` and `paths/` are scoped separately by their own AGENTS.md).

## Ownership

This package owns document assembly: `spec.go` (`Spec`, `ServeSpec`,
`ServeSwaggerUI`, `ServeReDoc`, the embedded viewer HTML), `prefix.go` (the
document header), and `components.go` plus its six `components_*.go`
siblings (the shared `components` block). It imports every family package
under `paths/` and `openapi/schema`; it MUST NOT be imported back by them —
`openapi/schema` exists specifically to give both sides a place to share a
fragment without that cycle (see `openapi/schema/AGENTS.md`).

- `Spec()` is a single ordered string concatenation. The order matches the
  published `paths` block; reordering it reorders the published document.
- Each `components_*.go` file is a single string-literal fragment
  concatenated by `components.go`. Split a new one by subject, matching the
  existing files, not by size.
- `docs/internal/naming.md` is law here: no `openapi_` file prefixes (the
  historical flat layout this package replaced) and no package-name
  stutter in exported identifiers.

## Verification

Any change to a fragment concatenated by `Spec()` MUST keep
`scripts/verify-openapi.sh` green. It cross-references `mux.HandleFunc`
registrations in `go/internal/query/` and `go/internal/serviceintelhttp/`
against the fragments under this tree, and understands both the historical
`openapi_paths_*.go` shape and the current `openapi/paths/**/*.go` shape
(#6642) — a route with no matching fragment, or a fragment with no
registered route, fails the gate.

Any wire-visible change (new route, changed schema, changed response
shape) MUST update `docs/public/reference/http-api.md` in the same PR, per
the root `CLAUDE.md` Documentation Discipline rule.

Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi` after any edit here.
