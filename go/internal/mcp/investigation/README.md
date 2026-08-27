# MCP investigation registration

## Purpose

This package owns five MCP tool definitions for investigation workflow
discovery, workflow resolution, and evidence-packet exports.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tools'
global positions, route resolution, HTTP dispatch, authorization, query
execution, response envelopes, transport, and telemetry. The parent also keeps
the split routing shape: workflow discovery and resolution stay in
`dispatch_investigation_workflows.go`, while the three packet exports stay in
`dispatch_investigation_packets.go`.

The HTTP query handlers remain responsible for selector validation, tenant
scope, source-fact bounds, evidence-packet composition, and response shaping.

## Exported surface

- `WorkflowTools` returns `list_investigation_workflows` followed by
  `resolve_investigation_workflow`.
- `PacketTools` returns `export_supply_chain_impact_packet`,
  `export_deployable_unit_packet`, and
  `export_cloud_runtime_drift_packet`, in that order.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by both constructors.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `investigation`, while the declared package is
  `investigationtools`. The root uses an explicit import alias.
- Both constructors return fresh definitions. A caller may modify one result
  without changing a later result.
- The combined serialized definitions are 4,824 bytes with SHA-256
  `393e7901eda034e7a18a8a043895e2cde337dc0b103f994126bcc7ae972b8a82`.
- The root registry keeps the workflow pair followed by the packet trio, after
  query playbooks and before semantic evidence. The complete registry remains
  162 tools in the same order.
- Workflow and packet registrations deliberately keep separate root routers.
- Keep query execution, route mapping, authorization, transport, and telemetry
  out of this package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, response shaping, transport, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Investigation workflows](../../../../docs/public/reference/investigation-workflows.md)
- [Investigation evidence packets](../../../../docs/public/reference/investigation-evidence-packet.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
