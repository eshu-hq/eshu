# AGENTS.md — codeprovenance

Scoped agent rules for `go/internal/codeprovenance`.

This package is the **single source of truth** for the ADR #2222 code-edge
resolution-provenance vocabulary. Treat the vocabulary as a closed contract.

- The `Method` constants, `confidenceByMethod`, and `reasonByMethod` MUST stay in
  lockstep. Every classified method needs both a confidence tier and a reason;
  `TestEveryConfidenceTierHasAReason` guards this.
- Do NOT change a tier number or method string without updating ADR #2222
  (`docs/internal/design/2222-resolution-provenance-code-edges.md`), the graph
  model reference docs, and the per-language accuracy goldens (#2226) in the same
  change. These values are a user-visible graph contract.
- Keep this a leaf package: no imports of `reducer`, `storage`, or `query`.
  Those packages import this one, never the reverse.
- Provenance is descriptive, not admissive. Nothing here may gate, add, drop, or
  filter an edge, or promote a heuristic score to canonical truth.
- `MethodUnspecified` exists for backward compatibility (legacy edges) and must
  remain `Valid` but not `Classified`. Emitters must produce a `Classified`
  method for every new edge.
- Run `go test ./internal/codeprovenance -count=1` after any change here.
