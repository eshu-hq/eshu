# Code OpenAPI Path Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/code/` (package `code`).

## Ownership

This leaf owns the OpenAPI path fragments for the code-search and
static-analysis routes: `Routes`, `Symbols`, `Quality`, `Security`,
`RouteToCaller`, `Graph`, `Flow`, `Owners`, `DeadCodeScan`, and, from the
single file `dead_code.go`, both `DeadCodeInvestigation` and
`CrossRepoDeadCode`. `openapi/spec.go` concatenates all ten identifiers.

- This package MUST NOT import `openapi` — the parent imports this
  package, and the reverse would cycle.
- `dead_code.go` declaring two constants is a deliberate exception, not a
  precedent to generalize. Do not treat "one constant per file" as a rule
  enforced anywhere in this package; check each file's actual `const`
  declarations before assuming the pattern holds, and do not add a second
  constant to any other file here without a reason as strong as
  `dead_code.go`'s (two closely related dead-code fragments that read
  better as one file than two).
- These are documentation fragments, not the routes themselves. A change
  to a real `code` handler's method, path, or request/response shape must
  be mirrored here or `scripts/verify-openapi.sh` will report drift.

## Verification

Keep `scripts/verify-openapi.sh` green after any change (see
`openapi/AGENTS.md` for what it checks), and update
`docs/public/reference/http-api.md` in the same PR for any wire-visible
change. Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/paths/code` after any edit here.
