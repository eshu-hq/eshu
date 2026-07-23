# AGENTS.md — internal/graph/edgetype guidance for LLM assistants

## Read first

1. `go/internal/graph/edgetype/README.md` — purpose, ownership boundary,
   exported surface, invariants
2. `go/internal/graph/edgetype/doc.go` — the godoc contract
3. `go/internal/graph/edgetype/edgetype.go` — `EdgeType`, the constant block,
   `registered`, `All`, `IsRegistered`
4. `go/internal/graph/edgetype/edgetype_test.go` — `TestEdgeTypeStringParity`
   (byte-identity pin) and registry-consistency tests
5. `go/internal/graph/edgetype/coverage_schema_test.go` —
   `TestNoUnregisteredEdgeLiteral`, the AST scan that binds every production
   Cypher literal to this registry

## Invariants

- An `EdgeType` constant's string value is the graph-wire contract and MUST be
  byte-identical to the historical Cypher relationship type. Never change a
  value to "clean it up"; that breaks stored edges and readers.
- The constant block, the `registered` slice, and the `TestEdgeTypeStringParity`
  table move together. The parity test fails if they drift.

## Common changes

- Adding an edge type: add the `EdgeType` constant (PascalCase name, exact
  ALL_CAPS_SNAKE value), append it to `registered`, and add the parity-table
  row. Run `go test ./internal/graph/edgetype -count=1`.
- A failing `TestNoUnregisteredEdgeLiteral` means new production Cypher names an
  unregistered edge type. Register it here rather than weakening the test.

## Anti-patterns

- Do NOT register the data-driven `AWS_*` / `GCP_*` cloud families or
  observability coverage types. They are runtime-synthesized open sets; the
  coverage test skips them on purpose via `skipDynamic`. `AWSLambdaFunctionUsesImage`
  (`AWS_lambda_function_uses_image`, issue #5450) is the one deliberate
  exception: `DomainAWSCloudImageMaterialization` resolves exactly one fixed
  relationship type through a closed single-member vocabulary (mirroring
  `CAN_ASSUME`, not the open `AWS_<raw relationship_type>` family), so it is
  registered as a real constant even though its lowercase suffix makes
  `skipDynamic` ignore it in the unregistered-literal scan.
- Do NOT add node labels here. Labels are PascalCase and live in
  `internal/graph`.

## Do not change without ADR review

- The graph-wire contract (any constant's string value). Renaming an edge type
  is a graph migration, not a refactor.
