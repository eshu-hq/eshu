# AGENTS.md - MCP query-playbook registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the ordered assembly position.
4. `../dispatch_query_playbooks.go` and the root query-playbook tests for route
   and body mapping.
5. `../toolcontract/README.md` for the dependency-neutral definition contract.
6. `../../../../docs/public/reference/query-playbooks.md` for the catalog and
   resolver contract.

## Invariants

- Keep this package registration-only. Routes and argument mapping stay in the
  parent MCP package; catalog and resolver behavior stay in `internal/query`.
- Keep the package clause as `package playbooktools`; the root imports it with
  an explicit alias.
- Preserve both names, descriptions, schemas, and local order.
- Return fresh definitions on every call.
- Keep the root pair after documentation tools and before investigation
  workflows.

## Common changes

- Change a schema only with the root route tests, query-handler contract,
  public query-playbook docs, and golden-corpus query-shape proof.
- Update the serialized-definition hash in `tools_test.go` only for an approved
  wire-contract change.
- Keep catalog content and playbook resolution logic out of registration.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.
- Moving `queryPlaybookRoute` here would pull root-only route and argument
  helpers across the ownership boundary.
- Reusing package-level maps or slices lets caller mutation leak into later
  `tools/list` responses.
- A set-only test misses swapping list and resolve or moving the pair away from
  its client-visible position.

## Anti-patterns

- Do not add route, HTTP, query, catalog, storage, authorization, summary, or
  telemetry helpers.
- Do not register tools through `init` functions.
- Do not weaken the serialized-definition or root ordered-name hash guards.
- Do not execute a query playbook from this package.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/playbooks ./internal/mcp -count=1
go vet ./internal/mcp/playbooks ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
query-playbook response-shape change also requires the golden-corpus gate
described by the parent package.
