# AGENTS.md — MCP visualization registration and route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `tools.go`, `tools_test.go`, `routes.go`, and `routes_test.go` for the child
   registration and request-selection contracts.
4. `../types.go` for the ordered assembly position.
5. `../dispatch_visualization.go` and
   `../dispatch_visualization_contract_test.go` for the root adapter and
   production-boundary proof.
6. `../toolcontract/README.md` and `../routecontract/README.md` for the
   dependency-neutral definition and request contracts.

## Invariants

- Keep only visualization family membership and pure argument-to-request
  selection here. Global route fanout, the private adapter, and execution stay
  in the parent MCP package and `internal/query`.
- Keep the package clause as `package visualizationtools`; the root imports it
  with an explicit alias.
- Preserve the tool name, description, and input schema.
- Return a fresh definition on every call.
- Preserve `POST /api/v0/visualizations/derive` with the exact `view`,
  `source_response`, and `source_truth` body fields. `view` accepts only a
  string; the two source fields pass through unchanged.
- Return the zero request and `handled=false` for unrelated tools.
- Keep derivation source-only: the caller supplies an already-authorized
  response, and this registration must not imply a new graph or content read.

## Common changes

- Change the schema only with the root dispatch tests, query-handler contract,
  public visualization docs, and golden-corpus query-shape proof.
- Change the route only with exact child request tests, the root adapter test,
  the shared HTTP contract, and applicable golden-corpus proof.
- Update the serialized-definition hash in `tools_test.go` only for an approved
  wire-contract change.
- Add a tool here only with an explicit decision about its position in the root
  registry.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` and
  `routecontract` only.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Reusing one package-level definition lets a caller mutation leak into later
  `tools/list` results.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root ordered-name hash guards.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/visualization ./internal/mcp -count=1
go vet ./internal/mcp/visualization ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
