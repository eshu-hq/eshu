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
- `dead_code_scan.go` — `DeadCodeScan`: the per-repository dead-code scan.
- `dead_code.go` — `DeadCodeInvestigation` (a single dead-code
  investigation) and `CrossRepoDeadCode` (the cross-repository rollup) —
  two exported constants in this one file, unlike every other file in this
  package. Do not read "one constant per file" as a rule this package
  enforces; it is a pattern most files happen to follow, not an invariant.

`openapi/spec.go` concatenates all ten identifiers, non-contiguously —
`code.Owners` in particular is added far later in `openapi.Spec()`'s concatenation
order, next to the CI/CD fragments, not alongside the other `code.*`
entries.

## Move evidence

These ten files moved here verbatim from the query root (Issue #6060 lane
C, #6642): `openapi_paths_code.go` -> `routes.go`,
`openapi_paths_code_symbols.go` -> `symbols.go`,
`openapi_paths_code_quality.go` -> `quality.go`,
`openapi_paths_code_security.go` -> `security.go`,
`openapi_paths_code_route_to_caller.go` -> `route_to_caller.go`,
`openapi_paths_code_graph.go` -> `graph.go`,
`openapi_paths_code_flow.go` -> `flow.go`,
`openapi_paths_codeowners.go` -> `owners.go`,
`openapi_paths_code_dead_code_scan.go` -> `dead_code_scan.go`, and
`openapi_paths_code_dead_code.go` -> `dead_code.go`. Only the package
clause and file names changed; the JSON each constant renders is unchanged.
