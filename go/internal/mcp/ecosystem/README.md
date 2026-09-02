# MCP ecosystem registration

## Purpose

This package owns the 23 MCP tool definitions assembled by the ecosystem
registration group. They cover ecosystem summaries, repository context,
infrastructure and impact reads, package-registry identity, and change planning.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains the tools'
global position, route resolution, HTTP dispatch, authorization, query
execution, response envelopes, transport, and telemetry.

Routing remains split in the parent package. Ecosystem summaries and change
planning enter through `dispatch_ecosystem.go`; repository reads enter through
`dispatch_repositories.go`, while package-registry request selection lives in
`../packageregistry` and reaches dispatch through the `packageRegistryRoute`
adapter; infrastructure-search request selection lives in `../infrasearch` and
reaches dispatch through the `infraResourceSearchRoute` adapter in
`dispatch_infra_search.go`, while the other infrastructure reads enter through
`dispatch.go`; impact-analysis request selection lives in `../impact` and
reaches dispatch through the `impactRoute` adapter in `dispatch_impact.go`;
environment comparison stays in `compareRoute`.

## Exported surface

- `Tools` returns all 23 definitions in their canonical local order.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/toolcontract` owns the dependency-neutral `ToolDefinition`
  shape returned by `Tools`.

## Telemetry

None. Registration only constructs in-memory data. The parent MCP package keeps
transport and dispatch signals, while the HTTP handlers retain
`eshu_dp_api_request_duration_seconds` and
`eshu_dp_api_request_errors_total`.

## Gotchas / invariants

- The import path ends in `ecosystem`, while the declared package is
  `ecosystemtools`. The root uses an explicit import alias.
- `Tools` returns fresh definitions. A caller may modify one result without
  changing a sibling definition or a later result.
- Preserve the 23 names, descriptions, schemas, and local order. `Tools` keeps
  one ordered assembly built from seven private definition slices and seven
  single-definition constructors. `package_registry_tools.go` owns the two
  package-registry definitions, while the other specialized sibling files own
  their single-definition helpers. `change_surface_tools.go` owns the
  `find_change_surface` and `investigate_change_surface` pair.
- The serialized definitions are 20,585 bytes with SHA-256
  `8dcb60e87971b24d53f1be68ccbc7657faa03a1378f34d92990833db0ab0284f`.
- The root keeps tools 41–63, between `get_repository_language_inventory` and
  `count_infra_resources`. The complete registry remains 162 tools in the same
  order.
- Keep routing, argument mapping, query execution, authorization, transport,
  and telemetry out of this package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, response shaping, transport, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [MCP impact-analysis route selection](../impact/README.md)
- [MCP infrastructure-search route selection](../infrasearch/README.md)
- [Source layout](../../../../docs/public/reference/source-layout.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
