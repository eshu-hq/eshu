# MCP query-playbook registrations

## Purpose

This package owns the MCP definitions for listing and resolving deterministic
query playbooks.

## Ownership boundary

This package owns registration data only. `internal/mcp` retains global tool
assembly, query-playbook routes, HTTP dispatch, authorization, response
envelopes, and transport telemetry. `internal/query` owns the catalog and
resolver handlers. Those handlers describe bounded call plans; they do not run
the calls while resolving a playbook.

## Exported surface

- `Tools` returns `list_query_playbooks` followed by
  `resolve_query_playbook`.

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

- The import path ends in `playbooks`, while the declared package is
  `playbooktools`. The root uses an explicit import alias.
- `Tools` returns fresh definitions. Mutating one result must not change a later
  result.
- Keep local order as list then resolve. The root keeps this pair after the
  documentation tools and before investigation workflows.
- Keep routing, catalog data, resolver execution, authorization, and telemetry
  out of this package.

No-Observability-Change: this extraction does not change routing, dispatch,
authorization, query execution, response shaping, or telemetry.

## Related docs

- [MCP package](../README.md)
- [MCP tool contract](../toolcontract/README.md)
- [Query playbooks](../../../../docs/public/reference/query-playbooks.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
