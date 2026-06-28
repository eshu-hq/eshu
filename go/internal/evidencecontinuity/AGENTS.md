# AGENTS.md - internal/evidencecontinuity guidance for LLM assistants

## Read First

1. `README.md` - package purpose and gate boundary.
2. `contract.go` - contract schema, finding taxonomy, and validation rules.
3. `load.go` - repository loading of the matrix and generated surface inventory.
4. `specs/evidence-continuity.v1.yaml` - the product conformance matrix.

## Invariants

- This package is a static contract verifier. Do not add runtime API, MCP,
  graph, or Postgres calls here.
- A row must name known capability-matrix capability IDs, generated API routes,
  and generated MCP tools.
- Negative evidence-loss cases stay closed over empty, missing, stale,
  truncated, and inaccessible evidence.
- Deterministic provider-key independence is explicit in the matrix. Semantic
  or provider-backed evidence may be referenced only as optional/labeled proof.

## Verification

Run `cd go && go test ./internal/evidencecontinuity -count=1` after changing
this package or `specs/evidence-continuity.v1.yaml`.
