# MCP documentation registrations

## Purpose

This package owns the six MCP tool definitions for documentation reads and
finding aggregates. It keeps their names, descriptions, and input schemas in a
leaf package below the MCP root.

## Ownership boundary

This package owns registration data only. `internal/mcp` still owns global tool
order, route resolution, HTTP dispatch, authorization, response envelopes, and
telemetry. Query and storage behavior remain in `internal/query` and its
storage dependencies.

## Exported surface

- `Tools` returns the four documentation read definitions.
- `FindingAggregateTools` returns the two documentation finding aggregate
  definitions.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape used by both constructors.

## Telemetry

None. Registration is in-memory data construction. MCP transport and dispatch
telemetry remain in `internal/mcp`.

## Gotchas / invariants

- The import path ends in `documentation`, while the declared Go package is
  `doctools`. The root uses an explicit import alias so registration ownership
  remains obvious at the assembly call sites.
- The parent registry inserts `Tools` and `FindingAggregateTools` at two
  different positions. Combining them changes the client-visible 162-tool
  order.
- Constructors return fresh definitions. Keep all six names, descriptions,
  input schemas, and their local order byte-for-byte compatible unless an MCP
  contract change is approved.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, or telemetry.

## Related docs

- [MCP package](../README.md)
- [Documentation updater and actuator contract](../../../../docs/public/reference/documentation-updater-actuator-contract.md)
- [MCP tool contract](../toolcontract/README.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
