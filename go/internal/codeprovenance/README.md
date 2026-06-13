# codeprovenance

Closed resolution-provenance vocabulary for code relationship edges
(ADR [`2222`](../../../docs/internal/design/2222-resolution-provenance-code-edges.md)).

## Why this package exists

A code call edge resolved by a repository-wide name guess used to be
indistinguishable from a SCIP-proven edge: both shipped with a hard-coded
`confidence = 0.95`. This package records *how* an edge was resolved as a closed
`Method` enum and derives the numeric `confidence` and human `reason` from that
method, so agents and operators can weight an individual relationship. The same
vocabulary now covers parser-declared inheritance and IMPLEMENTS rows that need
the same confidence derivation instead of relationship-local literals.

It is a leaf package imported by:

- `go/internal/reducer` — emits a `Method` on each code-call/reference/metaclass
  and inheritance/IMPLEMENTS materialization row (issues #2223 and #2350).
- `go/internal/storage/cypher` — derives `confidence` and `reason` from the
  method when writing the edge (issue #2224).
- `go/internal/query` — surfaces `confidence` and `resolution_method` on
  relationship/story answers (issue #2225).

## The vocabulary

Strongest to weakest, with the confidence each method derives:

| Method | Derived confidence | Resolver mechanism |
| --- | --- | --- |
| `scip` | 0.99 | SCIP semantic symbol resolution |
| `declared` | 0.95 | Explicitly declared in source (e.g. Python metaclass) |
| `same_file` | 0.95 | Same-file lexical scope or unique name |
| `import_binding` | 0.90 | Explicit import / package-qualified / re-export |
| `type_inferred` | 0.80 | Receiver/return-type inference, dynamic alias, constructor |
| `scope_unique_name` | 0.70 | Directory/package-scoped unique name, no import |
| `repo_unique_name` | 0.50 | Repository-wide unique-name fallback |
| `unspecified` | `LegacyConfidence` (0.95) | Not recorded: legacy or unclassified edge |

The set is **closed**. `same_file` keeps the historical 0.95 so the dominant
case is unchanged. `Confidence` and `Reason` fall back to the legacy/unspecified
values for unknown methods so un-reprojected edges keep their prior behavior.

## Operational notes

- `Method` is descriptive, never admissive: it does not add, drop, or filter
  edges.
- Per-edge provenance is orthogonal to the answer-level `TruthEnvelope`; it never
  raises or lowers the answer's truth level.
- Adding a resolver branch means extending `Method`, `confidenceByMethod`,
  `reasonByMethod`, and the per-language accuracy goldens (#2226) in the same
  change. An unmapped branch defaults to `unspecified` and fails the parity gate.
