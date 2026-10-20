# Canonical node package guidance

## Ownership

- This package owns the canonical node-phase Cypher templates, the
  retract label sets, the entity-metadata helpers, the code-target
  existence checker, and the statement-free parts for the canonical
  edge-domain families nested here under #6694: the structural families
  (`atlantis`, `gitlab`, `helm_template_value`, `kustomize`), the
  code/projection families (`code_call` with its retract, `instantiates`,
  `rationale`, `documentation`, `handles_route`, `runs_in`,
  `invokes_cloud_action`, `codeowners`, `submodule`, `deployable_unit`),
  and the repository-relationship templates (`relationships`).
- The parent `cypher` package owns `CanonicalNodeWriter`, the `Statement`
  wrap, and phase dispatch, and qualifies every symbol it uses. Each
  family's root seam maps `Part` onto `Statement` preserving operation,
  parameters, order, and Drain flags.
- `cmd/reducer` reads this package's `CypherReader` interface for the
  existence-probe wiring.
- `cmd/*` kustomize resolvers implement this package's
  `KustomizeOverlayResolver` port; the `edge/writer` leaf reads this
  package's templates and registries; the Ifá baseline gate reads
  `CodeCallMaterializedEdgeTypes` from here.

## Invariants

- Keep templates byte-identical to the reviewed move: the Cypher text is
  the proven hot-path shape, never reworded by a move.
- Family resolution returns `Part` values, never `Statement`: the
  `IsRetract`/`Drain` flags ride on the part and the root seam owns the
  `Operation` mapping, so retract-before-upsert order and the NornicDB
  autocommit routing survive the move unchanged.
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
