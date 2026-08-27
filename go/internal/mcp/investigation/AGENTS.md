# AGENTS.md — MCP investigation registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly positions.
4. `../dispatch_investigation_workflows.go` and
   `../dispatch_investigation_packets.go` for the split route ownership.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../query/AGENTS.md` before changing investigation query behavior.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package; validation and reads stay in `internal/query`.
- Keep the package clause as `package investigationtools`; the root imports it
  with an explicit alias.
- Preserve all five tool names, descriptions, schemas, and their local order.
- Return fresh definitions on every call.
- Keep the root order `query playbooks -> investigation workflows ->
  investigation packets -> semantic evidence` and the complete 162-tool order
  unchanged.
- Keep workflow routing in `dispatch_investigation_workflows.go` and packet
  routing in `dispatch_investigation_packets.go`.

## Common changes

- Change a schema only with the root route tests, query-handler contract,
  public API documentation, and the golden-corpus proof that replays saved
  inputs and checks the returned query shape.
- Update the serialized-definition hash in the package tests only for an
  approved wire-contract change.
- Add a tool only with an explicit decision about its position in the root
  registry and its owning router.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving either router here would expose root-only route and argument helpers
  and blur registration ownership.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses local or root registration reordering.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, transport, or telemetry
  helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root order guards.
- Do not merge the two root routers as part of a registration move.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/investigation ./internal/mcp -count=1
go vet ./internal/mcp/investigation ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
investigation query-shape change also requires the parent package's
golden-corpus gate, which replays saved inputs and checks the returned shape.
