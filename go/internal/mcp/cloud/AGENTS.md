# AGENTS.md — MCP cloud registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the two ordered assembly positions.
4. `../dispatch_cloud_inventory.go` and
   `../dispatch_cloud_runtime_drift.go` for argument mapping.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in
  the parent MCP package.
- Keep the package clause as `package cloudtools`; the root imports this
  registration package with an explicit alias.
- Preserve both tool names, descriptions, input schemas, and constructor order.
- Keep `InventoryTools` and `RuntimeDriftTools` separate so the root registry
  retains its existing assembly calls.
- Return fresh definitions. A caller may inspect or modify one result without
  changing a later result.
- Keep cloud aliases provider-specific and preserve the refusal to expose raw
  provider locators, identities, or credentials.

## Common changes

- Change a cloud tool schema only with the matching root dispatch test, HTTP
  handler contract, public documentation, and golden-corpus query-shape proof.
- Update the definitions hash in `tools_test.go` only for an approved wire
  contract change.
- Add a tool to the constructor that owns its established root assembly
  position, or get an explicit decision before changing global order.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving cloud routes here would split dispatch ownership and expose root-only
  route and argument helpers across the package boundary.
- Weakening provider-alias wording can make an AWS account number match a GCP
  project number with the same decimal value.

## Anti-patterns

- Do not add route, HTTP, query, storage, authorization, or telemetry helpers.
- Do not register tools through `init` functions.
- Do not weaken the metadata hash or root ordered-name test to make drift pass.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/cloud ./internal/mcp -count=1
go vet ./internal/mcp/cloud ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
