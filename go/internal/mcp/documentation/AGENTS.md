# AGENTS.md — MCP documentation registration guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `../types.go` for the two ordered assembly positions.
4. `../toolcontract/README.md` for the dependency-neutral definition contract.

## Invariants

- Keep this package registration-only. Routing and argument mapping stay in
  `internal/mcp/dispatch_documentation*.go`.
- Keep the package clause as `package doctools`. Go 1.26 ignores package files
  declared as `package documentation`.
- Preserve all six tool names, descriptions, input schemas, and constructor
  order.
- Keep `Tools` and `FindingAggregateTools` separate because the root registry
  inserts them at different positions.
- Return fresh definitions. Callers and tests may inspect or modify a returned
  slice without changing a later result.

## Common changes

- Change a documentation tool schema only with the matching root dispatch test,
  HTTP handler contract, public documentation, and golden-corpus query-shape
  proof.
- Add a documentation tool to the constructor that matches its existing root
  assembly position, or get an explicit decision before changing global order.
- Update the definitions hash in `tools_test.go` only for an approved wire
  contract change.

## Failure modes

- Declaring `package documentation` makes Go report that build constraints
  exclude every file in this directory.
- Combining both constructors moves the aggregate tools earlier in
  `ReadOnlyTools` and breaks the ordered-name contract.
- Importing the MCP root creates a parent-child cycle. Use `toolcontract` only.

## Anti-patterns

- Do not add route, HTTP, storage, authorization, or telemetry helpers here.
- Do not register tools through `init` functions.
- Do not weaken the metadata hash or root ordered-name test to make drift pass.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/documentation ./internal/mcp -count=1
go vet ./internal/mcp/documentation ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. Any intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
