# AGENTS.md - MCP router contract guidance

## Read first

1. `README.md` and `doc.go` in this directory, including the lane ownership
   table.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../contract/tool/README.md` for the dependency-neutral definition contract.
4. Epic #8752 for the decisions this contract encodes.

## Invariants

- No ranking, dispatch, or configuration reads here: the `Ranker` and
  `Dispatcher` are injected. Validation, the per-call credential filter, and
  router telemetry live here (#8757).
- `call_tool` must hand the caller's arguments to the `Dispatcher` untouched
  and never apply schema defaults; the root's byte-identity test fails
  otherwise.
- The credential filter may refuse only what the API auth middleware always
  refuses. Reuse the `query` route ledgers; never copy their entries.
- Keep metric labels bounded: `tool` is a registered name or `unregistered`.
- Never import `internal/mcp` (the root). The root injects a `Dispatcher`;
  `TestRouterDoesNotImportMCPRoot` fails on an import.
- `find_tool` and `call_tool` stay out of `mcp.ReadOnlyTools`; the root
  intercepts them before `dispatchTool`.
- Change `Query`, `Candidate`, `Ranker`, or `Dispatcher` only after commenting
  on epic #8752. Other lanes code against these signatures.
- Keep the package clause `package router`.
- Return fresh definitions from `Tools` and fresh slices from `CoreToolNames`.

## Common changes

- Change `coreToolNames` in `core.go` only from hosted call counts (#8759):
  update the decision record table and `measuredCoreToolNames` in
  `go/internal/mcp/core_tool_set_test.go` in the same change.
- A tool-schema change needs `tools_test.go` updated in the same change.
- `../tools_list_bytes_test.go` holds the default surface to the registry:
  `tools/list` must equal `ReadOnlyTools()` byte for byte. The surface switch
  (#8758) keeps that true for `full`; it needs no stored hash to update.

- A validator change needs `go test ./internal/mcp -run
  'Validator|RegisteredSchemas|RegisteredToolExamples|Routed'`: the
  differential test replays every literal dispatch-test argument map and the
  catalog sweep corpus.

## Failure modes

- Importing the MCP root creates a cycle.
- Treating an unknown surface value as `full` hides a misspelled setting.
- Sharing a package-level schema map lets a caller mutate later `tools/list`
  responses.
- A validator stricter than dispatch makes `call_tool` refuse a call a direct
  `tools/call` serves. That is an accuracy bug.

## Verification

From `go/`, run `go test ./internal/mcp/router/... ./internal/ask/catalog/...
-count=1` and `go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
