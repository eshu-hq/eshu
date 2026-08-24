# AGENTS.md — MCP visualization registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly position.
4. `../dispatch_visualization.go` for argument mapping.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package.
- Keep the package clause as `package visualizationtools`; the root imports it
  with an explicit alias.
- Preserve the tool name, description, and input schema.
- Return a fresh definition on every call.
- Keep derivation source-only: the caller supplies an already-authorized
  response, and this registration must not imply a new graph or content read.

## Common changes

- Change the schema only with the root dispatch tests, query-handler contract,
  public visualization docs, and golden-corpus query-shape proof.
- Update the serialized-definition hash in `tools_test.go` only for an approved
  wire-contract change.
- Add a tool here only with an explicit decision about its position in the root
  registry.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving the route here would split dispatch ownership and require exposing
  root-only argument helpers.
- Reusing one package-level definition lets a caller mutation leak into later
  `tools/list` results.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, or telemetry helpers.
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
