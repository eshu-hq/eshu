# Code OpenAPI Path Fragments

The OpenAPI path fragments for the code-search and static-analysis routes.

Layout:

- `routes.go` — `Routes`: the top-level code routes.
- `symbols.go` — `Symbols`: symbol search.
- `quality.go` — `Quality`: code quality signals.
- `security.go` — `Security`: security-relevant call paths.
- `route_to_caller.go` — `RouteToCaller`: route-to-caller tracing.
- `graph.go` — `Graph`: the call/reference graph.
- `flow.go` — `Flow`: data-flow tracing.
- `owners.go` — `Owners`: code ownership.

Dead-code detection (`Investigation`, `Scan`, `CrossRepo`) lives in the
`dead/` subpackage — see `dead/README.md`.

`openapi/spec.go` concatenates all eleven identifiers (eight here plus the
three in `dead/`), non-contiguously — `code.Owners` in particular is added
far later in `openapi.Spec()`'s concatenation order, next to the CI/CD
fragments, not alongside the other `code.*` entries.

## Move evidence

These eight files moved here verbatim from the query root (Issue #6060
lane C, #6642): `openapi_paths_code.go` -> `routes.go`,
`openapi_paths_code_symbols.go` -> `symbols.go`,
`openapi_paths_code_quality.go` -> `quality.go`,
`openapi_paths_code_security.go` -> `security.go`,
`openapi_paths_code_route_to_caller.go` -> `route_to_caller.go`,
`openapi_paths_code_graph.go` -> `graph.go`,
`openapi_paths_code_flow.go` -> `flow.go`, and
`openapi_paths_codeowners.go` -> `owners.go`. Only the package
clause and file names changed; the JSON each constant renders is unchanged.

Two more files, `openapi_paths_code_dead_code_scan.go` and
`openapi_paths_code_dead_code.go`, also moved here in that same pass, as
`dead_scan.go` (one constant, `DeadCodeScan`) and `dead.go` (two constants,
`DeadCodeInvestigation` and `CrossRepoDeadCode` — a deliberate exception to
the one-constant-per-file shape every other file in this package
followed). The owner then nested them as their own leaf, `dead/` (Issue
#6060 lane C, #6648), because all three exported names repeated the
parent package word mid-name (`code.DeadCodeScan`, `code.DeadCodeInvestigation`,
`code.CrossRepoDeadCode`); see `dead/README.md` for that move's evidence.
