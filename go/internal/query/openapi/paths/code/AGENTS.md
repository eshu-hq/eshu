# Code OpenAPI Path Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/code/` (package `code`).

## Ownership

This leaf owns the OpenAPI path fragments for the code-search and
static-analysis routes: `Routes`, `Symbols`, `Quality`, `Security`,
`RouteToCaller`, `Graph`, `Flow`, and `Owners`. Dead-code detection
(`Investigation`, `Scan`, `CrossRepo`) lives in the `dead/` subpackage —
see `dead/AGENTS.md`. `openapi/spec.go` imports both this package and
`dead` and concatenates all eleven identifiers (eight here plus three in
`dead/`).

- This package MUST NOT import `openapi` — the parent imports this
  package, and the reverse would cycle. It also MUST NOT import `dead` —
  `dead` never imports `code`, so nothing forces the reverse, but keeping
  it one-directional (spec.go depends on both, neither depends on the
  other) avoids inventing a coupling this split doesn't need.
- Each file here holds exactly one exported string constant; that pattern
  is now uniform across this package, unlike before the `dead/` split.
- These are documentation fragments, not the routes themselves. A change
  to a real `code` handler's method, path, or request/response shape must
  be mirrored here or `scripts/verify-openapi.sh` will report drift.

## Verification

Keep `scripts/verify-openapi.sh` green after any change (see
`openapi/AGENTS.md` for what it checks), and update
`docs/public/reference/http-api.md` in the same PR for any wire-visible
change. Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/paths/code` after any edit here.
