# OpenAPI Schema Fragments — Agent Instructions

Scope: `go/internal/query/openapi/schema/` (package `schema`).

## Ownership

This package holds OpenAPI JSON Schema fragments needed on both sides of an
import boundary that would otherwise cycle: `openapi/spec.go` imports every
family package under `openapi/paths/`, so a fragment shared between a
`components_*.go` file in `openapi` and a file in `openapi/paths/<leaf>`
cannot live in either of those packages without creating
`paths/<leaf> -> openapi -> paths/<leaf>`. This package imports neither
side, so both can import it.

- This is the single most important fact about this package: it exists
  because of that cycle, not as a general-purpose place for shared schema.
  Do not add a fragment here unless it genuinely has a consumer in
  `openapi` (or a `components_*.go` file) AND a consumer in some
  `paths/<leaf>` package, or two or more `paths/<leaf>` packages consume
  it — a leaf home would then force leaf-to-leaf imports. A fragment used
  by only one package belongs there instead.
- This package MUST NOT import `openapi` or any `openapi/paths/<leaf>`
  package — either import recreates the cycle this package exists to
  avoid.
- Every exported identifier is a raw JSON string fragment for
  concatenation, not a Go type. Keep that shape; do not introduce a
  `struct`/`json.Marshal` fragment here that the rest of this tree does not
  use.

## Verification

A change to a fragment here changes every document assembled by
`openapi.Spec()` that includes it — check that fragment's current
consumers before editing (`ImpactRuntimeTopologyLimits`:
`openapi/components_workload_session.go` and
`openapi/paths/impact/routes.go`; `EvidenceBoundaries`: the `impact`,
`repository` and `search` leaves), and keep `scripts/verify-openapi.sh`
green (see
`openapi/AGENTS.md` for what it checks). A wire-visible change MUST update
`docs/public/reference/http-api.md` in the same PR.

Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/schema` after any edit here.
