# AGENTS.md — MCP tool contract guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for the MCP wire, routing, authorization, and telemetry rules.
3. `../types.go` for the root alias and ordered tool assembler.

## Invariants

- Keep this package data-only. Do not add routing, dispatch, transport,
  authorization, telemetry, storage, or query behavior.
- Preserve the JSON field names of `ToolDefinition`; they are part of the MCP
  `tools/list` wire contract.
- Keep `mcp.ToolDefinition` as a type alias so existing callers remain
  source-compatible.
- Tool membership and ordering remain owned by `mcp.ReadOnlyTools`.
- A registration-family move must prove that the registered set, order, and
  schemas are unchanged.

## Common changes

- Add fields only when the MCP `tools/list` contract requires them, and update
  transport tests and public MCP documentation in the same change.
- Move a domain registration family by importing this package from the child
  and retaining its exact position in the root assembler.
- Update the ordered registration characterization whenever membership changes
  intentionally.

## Failure modes

- Replacing the root alias with a distinct type breaks source compatibility.
- Changing a JSON tag changes the client-visible tool schema.
- Moving assembly into this package can create parent-child import cycles or
  make registration order depend on package initialization.
- A set-only test can miss registration reordering.

## Anti-patterns

- Do not register tools through `init` functions here.
- Do not add route lookup, HTTP helpers, storage ports, or telemetry to make a
  family move compile.
- Do not accept a compile-only extraction proof without the ordered registry
  and schema tests.

## ADR-controlled changes

Changing the ownership of ordered assembly, switching to dynamic registration,
or merging transport behavior into this package requires an accepted
architecture decision before implementation.

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`. MCP wire-shape changes also require the B-7
golden-corpus proof described by the parent package instructions.
