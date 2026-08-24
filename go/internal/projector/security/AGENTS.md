# AGENTS.md — Security projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- Preserve the exact alert and security-group trigger kinds and earliest-match
  behavior.
- Preserve the shared AWS resource-materialization acceptance key for all three
  security-group phases.
- Keep payload validation in reducer handlers. A matching kind remains a
  projector trigger even when its payload is malformed.
- Do not move boundary validation, lookup construction, assembly, final sorting,
  queue writes, retries, graph writes, or telemetry into this package.
- Keep source-system selection bounded to source ref with collector fallback.

## Verification

Use TDD. Run focused security package and root assembly tests, ordered fan-out
parity and probe-count checks, package-doc verification, the full projector
package tree, selected path and telemetry mirrors, and the same-shape six-run
fan-out benchmark.
