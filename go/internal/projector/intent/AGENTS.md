# AGENTS.md — projector intent contract guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for projector-wide invariants.
3. `fact_lookup.go` and `../reducer_intent_fact_index.go` for the neutral index
   and root lifecycle wrapper.
4. `../scope_generation_intents.go` for ordered family assembly.

## Invariants

- Keep this package dependency-neutral. It must never import
  `internal/projector`.
- Preserve the root `projector.ReducerIntent` compatibility alias while
  callers still use it.
- `FactLookup` selection preserves original fact order. Kind argument order is
  not priority.
- Keep `FactLookup` concrete unless a replacement proves zero added allocations
  on the 44-probe fan-out benchmark.
- Keep lookup construction and lifetime, family assembly, projection lifecycle,
  queue writes, and telemetry in the root projector package. This package owns
  only the dependency-neutral lookup implementation and shared intent contract.

## Verification

Run the focused intent-contract and ordered fan-out tests, the fan-out benchmark,
the full projector package tests, `scripts/verify-package-docs.sh`, and
whole-module build and vet.
