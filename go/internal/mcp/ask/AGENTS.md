# AGENTS.md — MCP Ask registration and route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `tools.go`, `tools_test.go`, `routes.go`, and `routes_test.go` for the child
   registration and request-selection contracts.
4. `../types.go` for the ordered assembly position.
5. `../dispatch_ask.go` and `../dispatch_ask_test.go` for the root adapter and
   production-boundary proof.
6. `../toolcontract/README.md` and `../routecontract/README.md` for the
   dependency-neutral definition and request contracts.
7. `../../../../docs/internal/remote-validation/prod-ask-default-off.md` for
   deployed default-off and enabled-path evidence.

## Invariants

- Keep only Ask family membership and pure argument-to-request selection here.
  Global route fanout, the private adapter, execution, and default-off
  enforcement stay in the parent, `internal/query`, and server wiring.
- Keep the package clause as `package asktools`; the root imports it with an
  explicit alias.
- Preserve the `ask` name, description, question requirement, format enum, and
  default-off guidance.
- Return a fresh definition on every call.
- Preserve `POST /api/v0/ask` with a body containing both `question` and
  `format`; missing or non-string values map to empty strings.
- Return the zero request and `handled=false` for unrelated tools.
- Keep the root position after `trace_exposure_path` and before
  `list_relationship_edges`.

## Common changes

- Change the schema only with the root route tests, query-handler contract,
  public Ask docs, and golden-corpus query-shape proof.
- Change the route only with exact child request tests, the root production
  boundary test, the shared HTTP contract, and applicable golden-corpus proof.
- Update the serialized-definition hash in `tools_test.go` only for an approved
  wire-contract change.
- Change default-off wording only when the query handler, server wiring, and
  deployed evidence change with it.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` and
  `routecontract` only.
- Executing a request here would split authorization, timeout, response-budget,
  envelope, summary, and telemetry ownership.
- Reusing one package-level definition lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses moving `ask` away from its client-visible position.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, provider, storage,
  authorization, summary, or telemetry helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root ordered-name hash guards.
- Do not turn registration text into runtime feature enforcement.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/ask ./internal/mcp -count=1
go vet ./internal/mcp/ask ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
Ask response-shape change also requires the golden-corpus gate described by the
parent package.
