# AGENTS.md — projector intent contract guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for projector-wide invariants.
3. `../reducer_intent_fact_index.go` for the root implementation of
   `FactLookup`.
4. `../scope_generation_intents.go` for ordered family assembly.

## Invariants

- Keep this package dependency-neutral. It must never import
  `internal/projector`.
- Preserve the root `projector.ReducerIntent` compatibility alias while
  callers still use it.
- `FactLookup` selection preserves original fact order. Kind argument order is
  not priority.
- Do not move family assembly, indexing, projection lifecycle, queue writes, or
  telemetry into this package.

## Verification

Run the focused intent-contract and ordered fan-out tests, the full projector
package tests, `scripts/verify-package-docs.sh`, and whole-module build and vet.
