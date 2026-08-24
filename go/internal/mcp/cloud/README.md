# MCP cloud registrations

## Purpose

This package owns the two MCP tool definitions for provider-neutral cloud
inventory and runtime-drift reads. It keeps their names, descriptions, and
input schemas in a leaf package below the MCP root.

## Ownership boundary

This package owns registration data only. `internal/mcp` still owns global tool
order, route resolution, HTTP dispatch, authorization, response envelopes, and
telemetry. Query validation and storage reads remain in `internal/query` and its
storage dependencies.

## Exported surface

- `InventoryTools` returns the cloud resource inventory definition.
- `RuntimeDriftTools` returns the multi-cloud runtime-drift definition.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape used by both constructors.

## Telemetry

None. Registration only constructs in-memory data. MCP transport and dispatch
telemetry remain in `internal/mcp`, while the HTTP handlers retain the shared
API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `cloud`, while the declared package is `cloudtools`.
  The root uses an explicit alias so registration ownership is clear at the
  assembly call sites.
- Keep `InventoryTools` and `RuntimeDriftTools` separate. The parent registry
  owns their two positions in the client-visible 162-tool order.
- Constructors return fresh definitions. Keep both names, descriptions, input
  schemas, and their local order byte-for-byte compatible unless an MCP
  contract change is approved.
- Provider-specific aliases remain explicit: `account_id` requires AWS,
  `project_id` requires GCP, and `subscription_id` requires Azure. Registration
  must not expose raw provider locators or credentials.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Multi-cloud collector contract](../../../../docs/public/reference/multi-cloud-collector-contract.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
