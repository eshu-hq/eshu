# AGENTS.md - internal/evidencecontinuity guidance for LLM assistants

## Read First

1. `README.md` - package purpose and gate boundary.
2. `contract.go` - contract schema, finding taxonomy, and validation rules.
3. `load.go` - repository loading of the matrix and generated surface inventory.
4. `gatetriggers.go` - self-check that the evidence-continuity CI gate's
   triggers and workflow path filter span every package the proof refs name,
   every file the validator reads, and every package it is built from.
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
  `static-contract-gates.yml` are anchored, not assumed. Listing the input is
  convention, since nothing detects a read this package never declared; once it
  is listed, `gate_trigger_gap` enforces the trigger in both sets.
  Do not rely on a package trigger, or on another gate, to cover an input file
  incidentally — the package check probes only `_test.go` files in the package
  root, so narrowing such a trigger keeps that check green while dropping the
  input, and `checkPathFilterCoverage` compares the two trigger sets against
  each other, so a path removed from both is not a mismatch it can see.
- When this package starts importing another first-party package, add that
  package to `validatorCodeDeps` in `gatetriggers.go` in the same change, and
  give it a trigger in both sets above. The two invariants above watch what the
  validator reads; this one watches what it is built from. `cigates.MatchGlob`
  and `cigates.DornyFilters` decide what `gate_trigger_gap` reports, so a
  trigger set that does not name `go/internal/cigates` would let a semantic
  change there land without ever running this gate. This invariant is
  mechanically enforced end to end, unlike the input list above:
  `TestValidatorCodeDepsMatchRealImports` derives the set from this package's
  own source and fails on an unlisted import, and `gate_trigger_gap` then
  demands the trigger. This package's own directory is in the list for the same
  reason — no `go test` proof ref names it, so the package half of the check
  never demands it either. The boundary is first-party source: a third-party
  bump in `go/go.mod` or `go/go.sum` is not anchored, because those files name
  no package directory to watch and this validator does not read them. Such a
  bump still runs this package's tests in CI through the `code` filter in
  `test.yml`, which matches `**` outside docs and runs `go test ./...`.
- An input read as a directory gets two differently named probe paths, never
  one. A lone `specs/capability-matrix/a.yaml` probe accepts the trigger
  `specs/capability-matrix/a*.yaml`, which leaves every other fragment outside
  the gate's reach — the same filename-narrowing hole `packageTestProbes`
  closes for packages.

## Verification

Run `cd go && go test ./internal/evidencecontinuity -count=1` after changing
this package or `specs/evidence-continuity.v1.yaml`.
