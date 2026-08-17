# AGENTS.md - internal/evidencecontinuity guidance for LLM assistants

## Read First

1. `README.md` - package purpose and gate boundary.
2. `contract.go` - contract schema, finding taxonomy, and validation rules.
3. `load.go` - repository loading of the matrix and generated surface inventory.
4. `gatetriggers.go` - self-check that the evidence-continuity CI gate's
   triggers and workflow path filter span every package the proof refs name.
5. `specs/evidence-continuity.v1.yaml` - the product conformance matrix.

## Invariants

- This package is a static contract verifier. Do not add runtime API, MCP,
  graph, or Postgres calls here.
- A row must name known capability-matrix capability IDs, generated API routes,
  and generated MCP tools.
- Negative evidence-loss cases stay closed over empty, missing, stale,
  truncated, and inaccessible evidence.
- Deterministic provider-key independence is explicit in the matrix. Semantic
  or provider-backed evidence may be referenced only as optional/labeled proof.
- When a proof ref starts referencing a new Go package, add that package to
  the evidence-continuity triggers in `specs/ci-gates.v1.yaml` AND to the
  `evidence` filter in `.github/workflows/static-contract-gates.yml` in the
  same change; `gate_trigger_gap` findings enforce this.
- When `ValidateRepository` starts reading a new input file, add it to
  `validatorInputs` in `gatetriggers.go` in the same change, and give it
  a trigger in both sets above. An input the gate cannot see is a blind spot:
  an edit to it changes what this gate reports without ever running it. This
  includes the files the trigger check itself reads — the CI gate registry and
  `static-contract-gates.yml` are anchored, not assumed.
  Do not rely on a package trigger, or on another gate, to cover an input file
  incidentally — the package check probes only `_test.go` files in the package
  root, so narrowing such a trigger keeps that check green while dropping the
  input, and `checkPathFilterCoverage` compares the two trigger sets against
  each other, so a path removed from both is not a mismatch it can see.
- An input read as a directory gets two differently named probe paths, never
  one. A lone `specs/capability-matrix/a.yaml` probe accepts the trigger
  `specs/capability-matrix/a*.yaml`, which leaves every other fragment outside
  the gate's reach — the same filename-narrowing hole `packageTestProbes`
  closes for packages.

## Verification

Run `cd go && go test ./internal/evidencecontinuity -count=1` after changing
this package or `specs/evidence-continuity.v1.yaml`.
