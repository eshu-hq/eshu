# Canonical node package guidance

## Ownership

- This package owns the canonical node-phase Cypher templates, the
  retract label sets, the entity-metadata helpers, and the code-target
  existence checker.
- The parent `cypher` package owns `CanonicalNodeWriter`, the `Statement`
  wrap, and phase dispatch, and qualifies every symbol it uses.
- `cmd/reducer` reads this package's `CypherReader` interface for the
  existence-probe wiring.

## Invariants

- Keep templates byte-identical to the reviewed move: the Cypher text is
  the proven hot-path shape, never reworded by a move.
- A label that materializes but is missing from the retract sets is never
  retracted: delta and reconciliation projections strand the previous
  generation's nodes. Add new entity labels here in the same change that
  first writes them.
- Return fresh maps from helpers that build per-row property maps; callers
  must not observe shared package state. (The label sets themselves are
  read-only tables.)
- `Checker` is a pre-flight probe, not a writer: it runs a read-only
  existence query and writes nothing.
- Preserve parameterized UNWIND/MATCH/MERGE templates and their
  relationship identity. Cypher edits require the graph-truth and
  performance evidence in the root agent guidance.
- Never import the parent `cypher` package from this leaf.
