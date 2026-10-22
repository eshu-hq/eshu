# AGENTS.md - MCP router contract guidance

## Read first

1. `README.md` and `doc.go` in this directory, including the lane ownership
   table.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../contract/tool/README.md` for the dependency-neutral definition contract.
4. Epic #8752 for the decisions this contract encodes.

## Invariants

- Keep this package types and static data only. No ranking, validation,
  dispatch, configuration reads, authorization, or telemetry here.
- Never import `internal/mcp` (the root). The root injects a `Dispatcher`;
  `TestRouterDoesNotImportMCPRoot` fails on an import.
- `find_tool` and `call_tool` stay out of `mcp.ReadOnlyTools`; the root
  intercepts them before `dispatchTool`.
- Change `Query`, `Candidate`, `Ranker`, or `Dispatcher` only after commenting
  on epic #8752. Other lanes code against these signatures.
- Keep the package clause `package router`.
- Return fresh definitions from `Tools` and fresh slices from `CoreToolNames`.

## Common changes

- Fill `coreToolNames` in `core.go` only from hosted call counts (#8759).
- A tool-schema change needs `tools_test.go` updated in the same change.
- `../tools_list_bytes_test.go` holds the default surface to the registry:
  `tools/list` must equal `ReadOnlyTools()` byte for byte. The surface switch
  (#8758) keeps that true for `full`; it needs no stored hash to update.

## Failure modes

- Importing the MCP root creates a cycle.
- Treating an unknown surface value as `full` hides a misspelled setting.
- Sharing a package-level schema map lets a caller mutate later `tools/list`
  responses.

## Verification

From `go/`, run `go test ./internal/mcp/router/... ./internal/ask/catalog/...
-count=1` and `go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
