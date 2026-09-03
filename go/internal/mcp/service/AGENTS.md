# AGENTS.md — MCP service registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the three ordered assembly positions.
4. `../dispatch_repositories.go`, `../dispatch_service_catalog.go`, and
   `../dispatch_service_selector.go` (the `serviceContextRoute` adapter over
   `../servicecontext`) for split route ownership.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../query/AGENTS.md` before changing service query behavior.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in the
  parent MCP package; validation and reads stay in `internal/query`.
- Keep the package clause as `package servicetools`; the root imports it with
  an explicit alias.
- Preserve all five tool names, descriptions, schemas, and their local order.
- Return fresh definitions on every call, including independent nested maps and
  slices.
- Preserve the three root assembly positions and the complete 162-tool order.
- Keep service catalog routing (root) separate from service context, story,
  investigation, and intelligence-report routing (`../servicecontext`).

## Common changes

- Change a schema only with the root route tests, query-handler contract,
  public API documentation, and the golden-corpus proof that replays saved
  inputs and checks the returned query shape.
- Update the serialized-definition hash in the package tests only for an
  approved wire-contract change.
- Add a tool only with an explicit decision about its root registry position
  and owning router.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving a router here would expose root-only route and argument helpers and
  blur registration ownership.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses local or root registration reordering.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, transport, or telemetry
  helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root order guards.
- Do not combine or reorder the three constructor groups as part of a
  registration move.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/service ./internal/mcp -count=1
go vet ./internal/mcp/service ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
service query-shape change also requires the parent package's golden-corpus
gate, which replays saved inputs and checks the returned shape.
