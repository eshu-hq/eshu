# AGENTS.md — parser/fingerprint

## Ownership

Leaf-walk fingerprinting for function bodies. Called by the parser's
function-entity emission path; consumed downstream by the divergence
grouping queries (#6836) and the report (#6837+).

## Rules

- Tunables (`SketchRegs`, `ShingleK`, `LSHBands`, `LSHRows`) are frozen by
  the #6834 theory proof. Changing them invalidates the chosen similarity
  thresholds — re-prove first, per the Prove-The-Theory-First rule.
- Full-tier language additions need a classification table entry plus a
  `TestFullTierShapes` row and a comment/rename/litmus case. Unknown
  languages stay exact-only by default.
- Keep files under 500 lines. Go doc comments on all exported symbols.
- No source_cache reads here: the walker takes an already-parsed body node.
