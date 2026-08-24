# AGENTS.md — MCP Ask registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly position.
4. `../dispatch_ask.go` and `../dispatch_ask_test.go` for argument mapping.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../../../docs/internal/remote-validation/prod-ask-default-off.md` for
   deployed default-off and enabled-path evidence.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package; execution and default-off enforcement stay in
  `internal/query` and server wiring.
- Keep the package clause as `package asktools`; the root imports it with an
  explicit alias.
- Preserve the `ask` name, description, question requirement, format enum, and
  default-off guidance.
- Return a fresh definition on every call.
- Keep the root position after `trace_exposure_path` and before
  `list_relationship_edges`.

## Common changes

- Change the schema only with the root route tests, query-handler contract,
  public Ask docs, and golden-corpus query-shape proof.
- Update the serialized-definition hash in `tools_test.go` only for an approved
  wire-contract change.
- Change default-off wording only when the query handler, server wiring, and
  deployed evidence change with it.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving `askRoute` here would split dispatch ownership and expose root-only
  route helpers.
- Reusing one package-level definition lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses moving `ask` away from its client-visible position.

## Anti-patterns

- Do not add route, HTTP, query, provider, storage, authorization, summary, or
  telemetry helpers.
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
